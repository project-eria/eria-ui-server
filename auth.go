package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	zlog "github.com/rs/zerolog/log"
	"golang.org/x/crypto/argon2"
	_ "modernc.org/sqlite"
)

// Roles are provisional: permissions will move to an authorization service
// enforced by the Things themselves (PLAN §7).
const (
	RoleAdmin  = "admin"  // manages profiles, controls everything
	RoleMember = "member" // controls everything
	RoleGuest  = "guest"  // read only
)

const (
	sessionCookie   = "eria_session"
	sessionDuration = 30 * 24 * time.Hour
	// PINs are short: after maxFailures wrong PINs a profile is locked,
	// first for lockBase, then twice as long at each new failure (max lockMax).
	maxFailures = 5
	lockBase    = time.Minute
	lockMax     = time.Hour
)

var pinFormat = regexp.MustCompile(`^[0-9]{4,8}$`)

type Profile struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Role   string `json:"role,omitempty"`
	Pack   string `json:"pack,omitempty"`   // widget pack chosen by the profile, "" = Standard
	Theme  string `json:"theme,omitempty"`  // theme, "" = Ardoise
	Scheme string `json:"scheme,omitempty"` // auto | light | dark, "" = auto
}

type Auth struct {
	db       *sql.DB
	now      func() time.Time // overridable by tests
	PacksDir string           // where widget packs are dropped (PLAN §5.2)
}

type ctxKey struct{}

// OpenAuth opens (or creates) the SQLite database holding profiles and sessions.
func OpenAuth(path string) (*Auth, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // ponytail: one writer at a time is plenty for a household
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS profiles (
			id           INTEGER PRIMARY KEY,
			name         TEXT NOT NULL UNIQUE,
			role         TEXT NOT NULL CHECK (role IN ('admin', 'member', 'guest')),
			pin_hash     TEXT NOT NULL,
			failures     INTEGER NOT NULL DEFAULT 0,
			locked_until INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS sessions (
			token_hash TEXT PRIMARY KEY,
			profile_id INTEGER NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
			expires    INTEGER NOT NULL
		);`)
	if err != nil {
		return nil, err
	}
	a := &Auth{db: db, now: time.Now, PacksDir: "packs"}
	if err := a.migrateScreens(); err != nil {
		return nil, err
	}
	// Columns added after the first release: older databases get them here.
	for _, c := range []struct{ table, column string }{
		{"profiles", "pack"}, {"screens", "pack"},
		{"profiles", "theme"}, {"screens", "theme"},
		{"profiles", "scheme"}, {"screens", "scheme"},
	} {
		if err := a.addColumn(c.table, c.column, "TEXT NOT NULL DEFAULT ''"); err != nil {
			return nil, err
		}
	}
	return a, nil
}

func (a *Auth) addColumn(table, column, decl string) error {
	var n int
	if err := a.db.QueryRow(`SELECT count(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&n); err != nil || n > 0 {
		return err
	}
	_, err := a.db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, decl))
	return err
}

// Bootstrap creates a first admin profile when there is none.
func (a *Auth) Bootstrap(pin string) error {
	var n int
	if err := a.db.QueryRow(`SELECT count(*) FROM profiles`).Scan(&n); err != nil || n > 0 {
		return err
	}
	if pin == "" {
		return errors.New("no profile yet: set ERIA_UI_SERVER_ADMIN_PIN to create the first admin")
	}
	_, err := a.CreateProfile("Admin", RoleAdmin, pin)
	if err == nil {
		zlog.Warn().Msg("[auth:Bootstrap] First profile 'Admin' created: change its PIN")
	}
	return err
}

// --- PIN hashing (argon2id, OWASP parameters) ---

func hashPIN(pin string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	key := argon2.IDKey([]byte(pin), salt, 2, 19*1024, 1, 32)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=19$m=19456,t=2,p=1$%s$%s", b64.EncodeToString(salt), b64.EncodeToString(key))
}

func checkPIN(pin, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 {
		return false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil {
		return false
	}
	got := argon2.IDKey([]byte(pin), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// --- Profiles ---

func validRole(role string) bool {
	return role == RoleAdmin || role == RoleMember || role == RoleGuest
}

func (a *Auth) CreateProfile(name, role, pin string) (Profile, error) {
	name = strings.TrimSpace(name)
	if name == "" || !validRole(role) || !pinFormat.MatchString(pin) {
		return Profile{}, errInvalid
	}
	res, err := a.db.Exec(`INSERT INTO profiles (name, role, pin_hash) VALUES (?, ?, ?)`, name, role, hashPIN(pin))
	if err != nil {
		return Profile{}, errConflict // name already taken
	}
	id, _ := res.LastInsertId()
	return Profile{ID: id, Name: name, Role: role}, nil
}

func (a *Auth) Profiles() ([]Profile, error) {
	rows, err := a.db.Query(`SELECT id, name, role FROM profiles ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Profile{}
	for rows.Next() {
		var p Profile
		if err := rows.Scan(&p.ID, &p.Name, &p.Role); err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

// UpdateProfile changes the given fields; the last admin can't be demoted.
func (a *Auth) UpdateProfile(id int64, name, role, pin *string) error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if name != nil {
		if strings.TrimSpace(*name) == "" {
			return errInvalid
		}
		if _, err := tx.Exec(`UPDATE profiles SET name = ? WHERE id = ?`, strings.TrimSpace(*name), id); err != nil {
			return errConflict
		}
	}
	if role != nil {
		if !validRole(*role) {
			return errInvalid
		}
		if *role != RoleAdmin && a.isLastAdmin(tx, id) {
			return errLastAdmin
		}
		tx.Exec(`UPDATE profiles SET role = ? WHERE id = ?`, *role, id)
	}
	if pin != nil {
		if !pinFormat.MatchString(*pin) {
			return errInvalid
		}
		// New PIN: unlock, and sign out everywhere.
		tx.Exec(`UPDATE profiles SET pin_hash = ?, failures = 0, locked_until = 0 WHERE id = ?`, hashPIN(*pin), id)
		tx.Exec(`DELETE FROM sessions WHERE profile_id = ?`, id)
	}
	return tx.Commit()
}

func (a *Auth) DeleteProfile(id int64) error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if a.isLastAdmin(tx, id) {
		return errLastAdmin
	}
	tx.Exec(`DELETE FROM profiles WHERE id = ?`, id) // sessions cascade
	return tx.Commit()
}

func (a *Auth) isLastAdmin(tx *sql.Tx, id int64) bool {
	var role string
	var admins int
	tx.QueryRow(`SELECT role FROM profiles WHERE id = ?`, id).Scan(&role)
	tx.QueryRow(`SELECT count(*) FROM profiles WHERE role = 'admin'`).Scan(&admins)
	return role == RoleAdmin && admins <= 1
}

// --- Login, sessions ---

// Login checks a PIN; on success it returns a new session token. A locked
// profile answers errLocked with the remaining wait, whatever the PIN.
func (a *Auth) Login(id int64, pin string) (token string, wait time.Duration, err error) {
	var hash string
	var failures int
	var lockedUntil int64
	err = a.db.QueryRow(`SELECT pin_hash, failures, locked_until FROM profiles WHERE id = ?`, id).Scan(&hash, &failures, &lockedUntil)
	if err != nil {
		return "", 0, errBadPIN // unknown profile: same answer as a wrong PIN
	}
	now := a.now()
	if until := time.Unix(lockedUntil, 0); now.Before(until) {
		return "", until.Sub(now), errLocked
	}
	if !checkPIN(pin, hash) {
		failures++
		var lock int64
		if failures >= maxFailures {
			d := min(lockBase*time.Duration(math.Pow(2, float64(failures-maxFailures))), lockMax)
			lock = now.Add(d).Unix()
			zlog.Warn().Int64("profile", id).Int("failures", failures).Dur("lock", d).Msg("[auth:Login] Profile locked")
		} else {
			zlog.Warn().Int64("profile", id).Int("failures", failures).Msg("[auth:Login] Wrong PIN")
		}
		a.db.Exec(`UPDATE profiles SET failures = ?, locked_until = ? WHERE id = ?`, failures, lock, id)
		return "", 0, errBadPIN
	}
	raw := make([]byte, 32)
	rand.Read(raw)
	token = base64.RawURLEncoding.EncodeToString(raw)
	a.db.Exec(`UPDATE profiles SET failures = 0, locked_until = 0 WHERE id = ?`, id)
	a.db.Exec(`DELETE FROM sessions WHERE expires < ?`, now.Unix())
	_, err = a.db.Exec(`INSERT INTO sessions (token_hash, profile_id, expires) VALUES (?, ?, ?)`,
		tokenHash(token), id, now.Add(sessionDuration).Unix())
	return token, 0, err
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (a *Auth) session(r *http.Request) (Profile, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return Profile{}, false
	}
	var p Profile
	err = a.db.QueryRow(`SELECT p.id, p.name, p.role, p.pack, p.theme, p.scheme FROM sessions s JOIN profiles p ON p.id = s.profile_id
		WHERE s.token_hash = ? AND s.expires > ?`, tokenHash(c.Value), a.now().Unix()).Scan(&p.ID, &p.Name, &p.Role, &p.Pack, &p.Theme, &p.Scheme)
	return p, err == nil
}

func profileFrom(ctx context.Context) Profile {
	p, _ := ctx.Value(ctxKey{}).(Profile)
	return p
}

var (
	errInvalid   = errors.New("invalid name, role or PIN (4 to 8 digits)")
	errConflict  = errors.New("this name is already used")
	errLastAdmin = errors.New("the last admin can't be removed or demoted")
	errBadPIN    = errors.New("wrong PIN")
	errLocked    = errors.New("too many wrong PINs")
)

// --- HTTP ---

// Protect guards every /api route: a session is required except to list the
// profiles and log in; guests only read; profiles are managed by admins.
// Mutations must be JSON: browsers can't send that cross-site without a CORS
// preflight we never grant, which, with SameSite=Strict, blocks CSRF.
func (a *Auth) Protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if !strings.HasPrefix(path, "/api/") {
			next.ServeHTTP(w, r) // the app itself
			return
		}
		write := r.Method != http.MethodGet && r.Method != http.MethodHead
		if write && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			http.Error(w, "JSON expected", http.StatusUnsupportedMediaType)
			return
		}
		if path == "/api/auth/profiles" || path == "/api/auth/login" || path == "/api/health" {
			next.ServeHTTP(w, r)
			return
		}
		p, ok := a.session(r)
		switch {
		case !ok:
			http.Error(w, "not signed in", http.StatusUnauthorized)
			return
		case strings.HasPrefix(path, "/api/profiles") && p.Role != RoleAdmin,
			strings.HasPrefix(path, "/api/things/") && write && p.Role == RoleGuest:
			http.Error(w, "not allowed for this profile", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, p)))
	})
}

func (a *Auth) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/auth/profiles", func(w http.ResponseWriter, r *http.Request) {
		list, err := a.Profiles()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for i := range list {
			list[i].Role = "" // the login screen only needs names
		}
		writeJSON(w, list)
	})

	mux.HandleFunc("POST /api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Profile int64  `json:"profile"`
			PIN     string `json:"pin"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&in); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		token, wait, err := a.Login(in.Profile, in.PIN)
		switch {
		case errors.Is(err, errLocked):
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			http.Error(w, err.Error(), http.StatusTooManyRequests)
		case err != nil:
			http.Error(w, errBadPIN.Error(), http.StatusUnauthorized)
		default:
			http.SetCookie(w, &http.Cookie{
				Name: sessionCookie, Value: token, Path: "/",
				MaxAge: int(sessionDuration.Seconds()), HttpOnly: true,
				SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil,
			})
			w.WriteHeader(http.StatusNoContent)
		}
	})

	mux.HandleFunc("POST /api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(sessionCookie); err == nil {
			a.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, tokenHash(c.Value))
		}
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /api/me", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, profileFrom(r.Context()))
	})

	// Own preferences, for every role (guests included: it only changes the display).
	mux.HandleFunc("PUT /api/me", func(w http.ResponseWriter, r *http.Request) {
		// Only the fields sent are changed.
		var in struct{ Pack, Theme, Scheme *string }
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&in) != nil ||
			(in.Pack != nil && !packExists(a.PacksDir, *in.Pack)) ||
			(in.Theme != nil && !themes[*in.Theme]) || (in.Scheme != nil && !schemes[*in.Scheme]) {
			http.Error(w, "unknown pack, theme or scheme", http.StatusBadRequest)
			return
		}
		id := profileFrom(r.Context()).ID
		for column, value := range map[string]*string{"pack": in.Pack, "theme": in.Theme, "scheme": in.Scheme} {
			if value != nil {
				a.db.Exec(`UPDATE profiles SET `+column+` = ? WHERE id = ?`, *value, id)
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// Profile management (admin only, enforced by Protect).
	mux.HandleFunc("GET /api/profiles", func(w http.ResponseWriter, r *http.Request) {
		list, err := a.Profiles()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, list)
	})

	mux.HandleFunc("POST /api/profiles", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Name, Role, PIN string }
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&in); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		p, err := a.CreateProfile(in.Name, in.Role, in.PIN)
		if err != nil {
			httpError(w, err)
			return
		}
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, p)
	})

	mux.HandleFunc("PUT /api/profiles/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		var in struct{ Name, Role, PIN *string }
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&in); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if err := a.UpdateProfile(id, in.Name, in.Role, in.PIN); err != nil {
			httpError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("DELETE /api/profiles/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err := a.DeleteProfile(id); err != nil {
			httpError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func httpError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errInvalid):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, errConflict), errors.Is(err, errLastAdmin):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
