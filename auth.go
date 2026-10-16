package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	zlog "github.com/rs/zerolog/log"
	"golang.org/x/crypto/argon2"
	_ "modernc.org/sqlite"
)

// Levels are those of AUTH-DESIGN §2.1 (0-9, lower = more rights; 9 = read
// only, as when nobody is signed in), stored per profile until accounts move
// to the auth service.
const (
	levelAdmin  = 0
	notSignedIn = 9
)

const (
	sessionDuration = 30 * 24 * time.Hour
	// PINs are short: after maxFailures wrong PINs a profile is locked,
	// first for lockBase, then twice as long at each new failure (max lockMax).
	maxFailures = 5
	lockBase    = time.Minute
	lockMax     = time.Hour
)

var pinFormat = regexp.MustCompile(`^[0-9]{4,8}$`)

type Profile struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Level     int    `json:"level"`
	LastLogin int64  `json:"lastLogin,omitempty"` // unix seconds, 0 = never
}

type Auth struct {
	db  *sql.DB
	now func() time.Time // overridable by tests
}

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
			level        INTEGER NOT NULL CHECK (level BETWEEN 0 AND 9),
			last_login   INTEGER NOT NULL DEFAULT 0,
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
	a := &Auth{db: db, now: time.Now}
	if err := a.migrateLevels(); err != nil {
		return nil, err
	}
	for _, migrate := range []func() error{a.migratePages, a.migrateTerminals, a.migrateSettings} {
		if err := migrate(); err != nil {
			return nil, err
		}
	}
	return a, nil
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
	_, err := a.CreateProfile("Admin", levelAdmin, pin)
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

// migrateLevels turns the provisional roles of older databases into levels
// (admin 0, member 5, guest 9). The table is rebuilt with the foreign keys
// off, so sessions and pages are kept.
func (a *Auth) migrateLevels() error {
	var hasRole int
	a.db.QueryRow(`SELECT count(*) FROM pragma_table_info('profiles') WHERE name = 'role'`).Scan(&hasRole)
	if hasRole == 0 {
		return nil
	}
	a.db.Exec(`PRAGMA foreign_keys = OFF`)
	defer a.db.Exec(`PRAGMA foreign_keys = ON`)
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`CREATE TABLE profiles_levels (
			id           INTEGER PRIMARY KEY,
			name         TEXT NOT NULL UNIQUE,
			level        INTEGER NOT NULL CHECK (level BETWEEN 0 AND 9),
			last_login   INTEGER NOT NULL DEFAULT 0,
			pin_hash     TEXT NOT NULL,
			failures     INTEGER NOT NULL DEFAULT 0,
			locked_until INTEGER NOT NULL DEFAULT 0
		)`,
		`INSERT INTO profiles_levels (id, name, level, pin_hash, failures, locked_until)
			SELECT id, name, CASE role WHEN 'admin' THEN 0 WHEN 'member' THEN 5 ELSE 9 END, pin_hash, failures, locked_until FROM profiles`,
		`DROP TABLE profiles`,
		`ALTER TABLE profiles_levels RENAME TO profiles`,
	} {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func validLevel(level int) bool { return level >= 0 && level <= 9 }

func (a *Auth) CreateProfile(name string, level int, pin string) (Profile, error) {
	name = strings.TrimSpace(name)
	if name == "" || !validLevel(level) || !pinFormat.MatchString(pin) {
		return Profile{}, errInvalid
	}
	res, err := a.db.Exec(`INSERT INTO profiles (name, level, pin_hash) VALUES (?, ?, ?)`, name, level, hashPIN(pin))
	if err != nil {
		return Profile{}, errConflict // name already taken
	}
	id, _ := res.LastInsertId()
	return Profile{ID: id, Name: name, Level: level}, nil
}

func (a *Auth) Profiles() ([]Profile, error) {
	rows, err := a.db.Query(`SELECT id, name, level, last_login FROM profiles ORDER BY level, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Profile{}
	for rows.Next() {
		var p Profile
		if err := rows.Scan(&p.ID, &p.Name, &p.Level, &p.LastLogin); err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

// UpdateProfile changes the given fields; the last level-0 profile can't be
// demoted.
func (a *Auth) UpdateProfile(id int64, name *string, level *int, pin *string) error {
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
	if level != nil {
		if !validLevel(*level) {
			return errInvalid
		}
		if *level != levelAdmin && a.isLastAdmin(tx, id) {
			return errLastAdmin
		}
		tx.Exec(`UPDATE profiles SET level = ? WHERE id = ?`, *level, id)
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
	var level, admins int
	tx.QueryRow(`SELECT level FROM profiles WHERE id = ?`, id).Scan(&level)
	tx.QueryRow(`SELECT count(*) FROM profiles WHERE level = 0`).Scan(&admins)
	return level == levelAdmin && admins <= 1
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
	a.db.Exec(`UPDATE profiles SET failures = 0, locked_until = 0, last_login = ? WHERE id = ?`, now.Unix(), id)
	a.db.Exec(`DELETE FROM sessions WHERE expires < ?`, now.Unix())
	_, err = a.db.Exec(`INSERT INTO sessions (token_hash, profile_id, expires) VALUES (?, ?, ?)`,
		tokenHash(token), id, now.Add(sessionDuration).Unix())
	return token, 0, err
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Session returns the profile signed in with this token.
func (a *Auth) Session(token string) (Profile, bool) {
	var p Profile
	err := a.db.QueryRow(`SELECT p.id, p.name, p.level FROM sessions s JOIN profiles p ON p.id = s.profile_id
		WHERE s.token_hash = ? AND s.expires > ?`, tokenHash(token), a.now().Unix()).Scan(&p.ID, &p.Name, &p.Level)
	return p, err == nil
}

func (a *Auth) Logout(token string) {
	a.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, tokenHash(token))
}

var (
	errInvalid   = errors.New("invalid name, level (0-9) or PIN (4 to 8 digits)")
	errConflict  = errors.New("this name is already used")
	errLastAdmin = errors.New("the last level-0 profile can't be removed or demoted")
	errBadPIN    = errors.New("wrong PIN")
	errLocked    = errors.New("too many wrong PINs")
	errUnknown   = errors.New("unknown terminal, profile or Thing")
)

// --- HTTP ---
