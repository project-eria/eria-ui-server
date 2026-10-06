# ui-user — plan de conception

> Interface utilisateur pour piloter les Things ERIA (W3C WoT).
> Statut : **lots 1-11 faits** (2026-10-06) — accessibilité CONDITIONAL (vérifications humaines, REPORT.md §3) ; lot 8 : dépôt GitHub, tag et déploiement à faire par Cédric.

## 1. Philosophie

- **Contrôler, pas administrer.** ui-admin découvre/configure/déploie ; ui-user sert à allumer,
  ouvrir, lire une température, changer de mode. Rien de technique visible par défaut.
- **Le TD est la vérité.** L'UI se construit à partir des Thing Descriptions : aucun widget
  codé pour un appareil précis, seulement pour une *capacité* (LightDimmer, ShutterPosition…),
  avec repli générique piloté par le schéma de données.
- **Widgets autonomes.** Chaque widget est un web component (Lit) réutilisable hors de
  ui-user (hass, ui-admin, page statique). La coquille SvelteKit ne fait que naviguer.
- **Un seul point d'entrée réseau.** Le navigateur ne parle qu'au backend ui-user, jamais
  directement aux Things (mDNS impossible en navigateur, protection des ESP32).
- **Accessible par construction.** WCAG 2.2 AA selon A11Y.md ; mêmes widgets au doigt, au
  clavier et au lecteur d'écran.
- **Téléphone, tablette murale, ordinateur** : une seule UI responsive, pas de versions séparées.
- **Des services, pas des apps.** L'utilisateur voit des lampes, des volets, des modes —
  jamais les apps qui les exposent (gateway-knx, App Controller, orchestrateur).
- **Écrans prédéfinis + écrans personnalisés**, mélangés dans une même navigation (§5).

## 2. Architecture

```
navigateur
 ├─ SvelteKit (routes, layout, navigation, login)
 │    └─ <eria-light>, <eria-shutter>, <eria-sensor>, <eria-context>…   (Lit)
 │          ↑ reçoivent un objet ThingHandle, pas d'URL
 └─ client TS (1 module) ── REST /api/* + 1 WebSocket /api/ws ──┐
                                                                ▼
backend Go « ui-user » (app eria-core, déployable via le catalogue)
 ├─ découverte mDNS _wot._tcp (+ /eria à côté de chaque app) + cache des TD
 ├─ détection des capacités (catalogue eria-core/model)
 ├─ multiplexeur WS : N observations amont ⇄ 1 WS par navigateur (refcount)
 ├─ proxy lecture / écriture / actions
 ├─ comptes (profil + PIN), sessions, rôles
 ├─ SQLite (comptes ; plus tard pièces/organisation)
 └─ sert le build statique SvelteKit
```

## 3. Constat technique (ce que le code impose)

| Constat | Conséquence pour ui-user |
|---|---|
| Pas de Thing Directory ; le maître ne donne pas les URLs des TD | Le backend parcourt le mDNS lui-même |
| `@type` absent des TD Go (**vérifié** sur les TD réels le 2026-10-05) | Capacités détectées **par signature** (propriétés présentes), `@type` utilisé s'il existe. À corriger dans eria-core (hors session) |
| Aucune propriété marquée `readOnly` ; or dans gateway-knx, écrire `on`/`brightness`/`position` change seulement la valeur en cache, **sans piloter l'équipement** (gestionnaires par défaut) | Toutes les propriétés de capacité sont en lecture seule côté ui-user ; **les commandes passent par les actions** (`toggle`, `fade`, `open`/`close`/`stop`, `setPosition`) |
| WS maison : 1 socket par propriété, valeur JSON brute | Le backend mutualise ; 1 seule observation amont par propriété quel que soit le nombre de clients |
| PUT/POST exigent `Content-Type: application/json` | Respecté par le proxy |
| Pas d'événements HTTP ; pas d'événements dans le catalogue | v1 sans événements |
| `title` pas toujours lisible (KNX : id en title, nom en description) | Libellé = `description` si `title` ressemble à un id ; renommage local plus tard |
| `nosec` partout, CORS `*` | Voir §7 : la sécurité réelle est hors de l'UI |
| Unités hétérogènes (`°C` en Go, `celsius` sur ESP32) | Table de normalisation des unités côté backend |
| Chaque app expose aussi un App Controller (ref `eria`, id `eria:app:controler:*`) ; le maître et les agents sont des Things | Filtrés par le backend : jamais exposés à l'UI |
| Un Thing peut porter plusieurs capacités (ex. piscine : pH, ORP, température) | L'unité affichée est le **service** = (Thing, capacité), pas le Thing |

## 4. Technique

### Versions (2026-10-05)
- Go 1.26 (go.work), `eria-core` / `go-wot` du workspace, `modernc.org/sqlite` (Go pur, cross-compilation ARM).
- SvelteKit 3.0, Svelte 5.57, `@sveltejs/adapter-static` 4, Vite 8.3, Lit 3.3, Paraglide JS 2.25 (FR/EN, comme ui-admin), TypeScript 6.0 (SvelteKit 3 n'accepte pas encore TS 7).
- Tests : `go test`, Vitest, Playwright + axe-core.

### Arborescence
```
src/ui-user/
  PLAN.md
  backend/            module Go autonome (eria-core / go-wot **publiés**, hors go.work), embarquera frontend/build via go:embed
  frontend/           un seul package pnpm
    src/routes/       SvelteKit (coquille)
    src/lib/client/   client TS (REST + WS, ThingHandle)
    src/widgets/      web components Lit — aussi buildés en bundle autonome eria-widgets.js
    messages/         Paraglide fr/en
```

### API backend ⇄ navigateur
REST (cookie de session HttpOnly, SameSite=Strict) :
- `GET  /api/auth/profiles` — liste des profils (nom, avatar), sans secret
- `POST /api/auth/login` `{profile, pin}` · `POST /api/auth/logout` · `GET /api/me`
- `GET  /api/services` — services : `{id: "<thingId>#<capacité>", thing, capability, label, online, properties{}, actions{}}`
  (App Controllers, maître et agents exclus ; propriétés hors capacité → service `generic`)
- `GET|PUT /api/things/{id}/properties/{name}` · `POST /api/things/{id}/actions/{name}`
- `GET /api/screens` · `PUT /api/screens/{id}` (avec `version`, 409 si conflit) — écriture : `editRole` pour les partagés, propriétaire pour les personnels

WebSocket `/api/ws` (enveloppe JSON) :
- client → `{op:"observe"|"unobserve", thing, property}`
- serveur → `{thing, property, value}` · `{thing, online:bool}` · `{op:"things-changed"}`

### Contrat des widgets
- Entrée : propriété JS `.thing` (ThingHandle : `read`, `write`, `invoke`, `observe` → désabonnement)
  et, pour les widgets génériques, `.property` / `.action`. Aucun accès réseau direct.
- Sortie : rendu de l'état + commandes ; erreurs signalées par un évènement DOM
  `eria-error` que la coquille affiche.
- Attributs d'affichage : `variant` (`tile` | `full` | `value`), `label` (libellé
  personnalisé), `props` (propriétés affichées), `confirm` (booléen). Détail en §5.1.
- Choix du widget : fonction pure `widgetFor(service)` → `{tag, variants permises}`.
- v1 : `eria-light` (on/off, variation), `eria-shutter` (ouvrir/fermer/stop/position),
  `eria-garage`, `eria-sensor` (toutes les capacités capteur), `eria-trigger`,
  `eria-context` (manager-contexts) ; génériques `eria-switch`, `eria-number`, `eria-value`,
  `eria-button`.
- Mise à jour optimiste interdite : l'état affiché est toujours celui renvoyé par l'observation
  (évite les interrupteurs qui mentent).

### Accessibilité (A11Y.md, profil Standard AA)
- Contrôles natifs dans le shadow DOM (`button`, `input type=range`), jamais de `div` cliquable.
- Patterns APG : interrupteur = `button aria-pressed` ou switch ; volet/variateur = slider.
- Cibles ≥ 44×44 px (usage tablette/doigt), focus visible, `forced-colors`, `prefers-reduced-motion`.
- Changements d'état distants annoncés sobrement (pas de live region par capteur : bruit).
- Livrables : `REPORT.md` + `A11Y-DECISIONS.md` comme ui-admin ; vérifs humaines (VoiceOver, zoom 200 %).

## 5. Écrans

Deux sortes d'écrans, rangés dans **une seule navigation** ordonnée (barre du bas sur téléphone,
rail latéral sur tablette/ordinateur).

**Brique commune : la section.** Une section est un bloc prêt à l'emploi, codé en Svelte :
« Modes », « Lumières », « Ouvrants » (volets + garage), « Climat » (capteurs)… Elle se remplit
seule avec les services de la bonne capacité.

| | Écran prédéfini | Écran personnalisé |
|---|---|---|
| Défini par | le code (route SvelteKit) | l'utilisateur, en mode édition |
| Contenu | liste fixe de sections, disposition fixe | grille de **cartes** |
| Stockage | aucun | SQLite (`screens` : owner nul = partagé, sinon profil ; `editRole` ; `version` ; layout JSON) |
| Exemples v1 | Accueil (Modes + Climat), Lumières, Ouvrants, Climat, Modes | « Salon » (partagé), « Ma chambre » (personnel)… |

**Cartes** (écrans personnalisés) :
- `service` — un service précis, rendu par son widget ;
- `section` — une section prédéfinie insérée telle quelle (c'est ce qui permet de mélanger
  prédéfini et personnalisé dans un même écran) ;
- `title` — un intertitre (structure `h2`, utile au lecteur d'écran).

Écrans personnalisés **partagés** (visibles par tous) et **personnels** (visibles et
modifiables par leur profil seulement). Navigation : prédéfinis, puis partagés, puis personnels.

**Droit de modification par page** : chaque écran partagé porte un `editRole`, le rôle
minimum pour modifier sa configuration (page et widgets). Par défaut `admin` ; peut être
abaissé à `member` page par page (ex. « Salon » modifiable par tous les membres, « Tablette
entrée » réservé à l'admin). Vérifié **côté backend** à l'enregistrement, pas seulement en
masquant le bouton. Comme les rôles sont provisoires (§6), `editRole` sera remplacé par une
permission du futur service d'autorisation.

**Mode édition** (explicite, une page à la fois) :
- bouton **Modifier la page**, affiché seulement si le profil a le droit ;
- en édition, les widgets sont **inertes** (aucune commande envoyée aux Things), chaque carte
  montre ses boutons : Réglages (variante, taille, libellé, propriétés, confirmation),
  Monter, Descendre, Supprimer ; un bouton **Ajouter une carte** ouvre le choix du service
  puis de la variante ;
- réglages de la page : titre, icône, `editRole` (écrans partagés, admin seulement),
  position dans la navigation ;
- les changements forment un **brouillon** : **Enregistrer** (un seul `PUT`, atomique) ou
  **Annuler** ; quitter la page avec un brouillon non enregistré demande confirmation ;
- conflit : si quelqu'un a enregistré entre-temps (version de l'écran), l'enregistrement
  est refusé avec un message, pas d'écrasement silencieux ;
- entrée et sortie du mode annoncées (live region polie), focus ramené sur le bouton
  **Modifier la page** à la sortie.

Le réordonnancement se fait par **boutons Monter / Descendre** (alternative obligatoire au
glisser-déposer, WCAG 2.5.7) ; le glisser-déposer viendra éventuellement en plus.

Service disparu (Thing hors ligne ou retiré) : la carte reste, marquée « indisponible ».

### 5.1 Widgets dans les écrans personnalisés

**Grille** : CSS Grid, colonnes de taille fixe en nombre variable selon la largeur
(`repeat(auto-fill, minmax(80px, 1fr))`, 4 colonnes minimum sur téléphone). Chaque carte occupe
`grid-column: span W` / `grid-row: span H`. **Pas de `grid-auto-flow: dense`** : l'ordre visuel
reste l'ordre du DOM, donc du clavier et du lecteur d'écran (WCAG 1.3.2 / 2.4.3). Largeur
max d'une carte = 4 colonnes, pour tenir sur téléphone sans débordement.

**Variantes** (choisies par l'utilisateur) et **tailles permises** pour chacune :

| Variante | Rôle | Tailles (L×H) |
|---|---|---|
| `value` | la valeur seule (21,4 °C) — capteurs surtout | 1×1, 2×1, 2×2 |
| `tile` | icône + nom + état ; un appui = action principale (allumer, ouvrir…) ; bouton secondaire **Détails** → carte complète dans un `<dialog>` modal | 1×1, 2×1 |
| `full` | toutes les commandes (variateur, position, stop…) | 2×2, 4×2, 4×3 |

Le widget s'adapte à sa case par **container queries** CSS (fonctionnent dans le shadow DOM) :
la taille change la mise en page, jamais les fonctions disponibles d'une variante.
Chaque widget déclare ses variantes et tailles ; l'éditeur ne propose que celles-là.

**Réglages par carte** :
- **Libellé personnalisé** (remplace le nom du service, sur cette carte seulement) ;
- **Propriétés affichées** (services à plusieurs valeurs, ex. anémomètre force + angle) ;
- **Confirmation** avant action : `<dialog>` natif « Ouvrir la porte du garage ? » avec focus
  sur Annuler. Activée par défaut pour `GarageDoor`.

Modèle stocké (élément du layout JSON d'un écran) :
```json
{"kind": "service", "service": "urn:…#ShutterPosition", "variant": "full", "w": 2, "h": 2,
 "label": "Volet salon", "props": ["position"], "confirm": false}
```

Accessibilité propre aux widgets :
- tuile = un `button` (action principale, `aria-pressed` si bascule) + un `button` « Détails »
  distinct ; jamais deux actions sur le même élément (pas d'appui long caché) ;
- le nom accessible reprend le libellé personnalisé ;
- dialogues natifs (`showModal`) : piège et retour du focus fournis par le navigateur.

Autres cartes (groupe de services, lien vers un écran, texte, horloge…) : **voulues, à définir
plus tard** ; le champ `kind` du modèle les accueillera sans migration.

### 5.2 Packs de widgets (lot 10, décidé le 2026-10-06)

Un **pack** est une famille complète de widgets de même style (pas seulement des couleurs :
ses propres composants, styles, polices). « Thème » écarté : il laisse croire que seule
l'apparence change.

- **Forme** : dossier `packs/<id>/` sur le serveur (avec l'orchestrateur : `{data}/packs`),
  ajouté **sans recompiler** : `pack.json` (`id`, `name`, `version`, `sdk: 1`, capacité →
  balise, variantes/tailles, `report` optionnel) + `index.js` (module ES qui définit ses
  éléments, balises préfixées par l'`id`) + CSS, polices, images. `GET /api/packs`, fichiers
  servis sous `/packs/<id>/…`.
- **SDK** : ui-user publie `/sdk/eria-widgets.js` à URL stable (classe de base : suivi des
  propriétés, commandes, confirmation, `eria-error` ; Lit ; formatage valeurs/unités). Un pack
  n'écrit que le rendu et partage le Lit de l'app. Version `sdk` vérifiée ; contrat documenté
  (propriétés `thing`, `schemas`, `label`, `variant`, `confirm`, `viewonly`, `online`,
  `props`, `modes` ; évènements ; variantes et tailles ; **apparence uniquement par les
  variables `--eria-*` du §5.3**, pour que les thèmes s'appliquent à tout pack).
- **Choix** : pack de l'écran personnalisé > pack du profil > **Standard** (pack par défaut,
  les widgets actuels). Écrans prédéfinis : pack du profil. Capacité absente du pack → widget
  Standard ; pack absent ou en erreur → Standard + message.
- **Sécurité** : un pack est du JavaScript exécuté dans l'app, avec les droits de la personne
  connectée. Installation par un admin (accès serveur) ; liste des packs installés visible.
- **Accessibilité** : non garantie pour un pack ajouté. `pack.json` peut pointer vers son
  `REPORT.md` ; sans rapport, le pack est signalé « non vérifié » au choix. Standard reste
  toujours sélectionnable (repli accessible).
- **À trancher au lot 10** : téléversement depuis l'UI (admin) en plus du dépôt de fichiers ;
  rendu des sections par un pack.

### 5.3 Thèmes (lot 11, décidé le 2026-10-06)

Un **thème** change l'**aspect** des widgets (couleurs, polices, arrondis, espacements…),
pas leur structure : c'est le rôle du pack (§5.2). Les deux se combinent.

- **Prédéfinis et livrés avec l'UI**, jamais ajoutés à chaud : chaque thème est vérifié avec
  la release (contrastes recalculés, axe), et inscrit dans REPORT.md.
- **Variables CSS seulement** : un thème est un jeu de valeurs pour les variables `--eria-*`,
  qui traversent le shadow DOM. Jeu à élargir au lot 10 (aujourd'hui : couleurs + arrondi) :
  polices (texte, chiffres), échelle de tailles, espacements, ombres, épaisseur du focus.
  Le contrat du SDK impose aux packs de s'en servir.
- **Clair et sombre** : chaque thème fournit les deux ; réglage séparé **Auto** (système) /
  **Clair** / **Sombre**, par profil et par écran (ex. tablette murale toujours sombre).
- **Choix** : thème de l'écran personnalisé > thème du profil > **Ardoise** (thème par
  défaut, l'actuel). Écrans prédéfinis : thème du profil.
- **Noms** : « Ardoise » = thème par défaut ; « Standard » = pack par défaut.

## 6. Comptes et rôles (v1)

- **Profil + PIN** : on choisit son profil, on tape un PIN. PIN haché argon2id.
- **Anti-force brute obligatoire** (PIN court) : délai croissant + verrouillage par profil
  après N échecs, journalisé.
- Sessions : cookie HttpOnly, durée normale (la tablette se reconnecte comme les autres).
- Rôles **provisoires** : `admin` (gère les profils), `member` (contrôle), `guest` (lecture).
  À supprimer quand le service d'autorisation existera.
- Amorçage : si aucun profil, le backend crée un admin à partir d'un PIN donné en config/CLI.

## 7. Sécurité : la limite assumée

Le contrôle d'accès de ui-user est un **contrôle d'usage**, pas une sécurité : les Things en
`nosec` restent pilotables directement sur le LAN. Décision : **les autorisations vivront dans
un service tiers**, appliquées par les Things eux-mêmes (ex. jetons bearer/OAuth2 vérifiés par
go-wot et rs-wot), pour empêcher le contournement de l'UI.

Préparation côté ui-user, sans rien construire d'avance : toutes les requêtes vers les Things
passent par **une seule fonction** du backend, où l'ajout du jeton de l'utilisateur se fera.

## 8. Plan d'implémentation

| Lot | Contenu | Fini quand |
|---|---|---|
| 1 | Backend : découverte mDNS, cache TD, filtrage des apps, détection capacités → services, `GET /api/services`, proxy PUT/POST | `curl /api/services` sur la machine de test liste les services du banc, sans aucun App Controller |
| 2 | Backend : multiplexeur WS (refcount, reconnexion amont, statut online) | 2 navigateurs observent la même propriété → 1 seule socket amont |
| 3 | Frontend : squelette SvelteKit, navigation, client TS, widgets génériques | On pilote une lumière de test au clavier et au doigt |
| 4 | Widgets de capacité (lumière, volet, garage, capteurs, trigger) + page de démo | Chaque widget passe axe-core et la revue clavier |
| 5 | Sections + écrans prédéfinis (Accueil, Lumières, Ouvrants, Climat, Modes) ; section Modes (manager-contexts) | Activer un mode depuis l'Accueil (« nuit » n'existe pas : testé avec « Invités ») |
| 6 | Comptes : profils, PIN, sessions, rôles, anti-force brute | Un invité ne peut pas écrire (testé) |
| 7 | Écrans personnalisés : SQLite, cartes service/section/titre, mode édition, ordre de la navigation | Un admin crée « Salon » (partagé) et un membre « Ma chambre » (personnel, invisible aux autres), au clavier |
| 8 | Déploiement **par l'orchestrateur** (catalogue eria) : dépôt `project-eria/ui-user`, release par tag, déploiement depuis ui-admin (nœud choisi au déploiement) | UI déployée depuis l'onglet Catalogue, joignable depuis le téléphone |
| 9 | A11y : REPORT.md, décisions, vérifications humaines | Statut PASS ou CONDITIONAL documenté |
| 10 | **Packs de widgets** : SDK publié par l'app (contrat de variables `--eria-*` élargi), packs ajoutables sans recompiler, choix par profil et par écran (§5.2) | Un pack d'exemple installé sur le serveur, choisi pour un écran ; capacité manquante → repli sur Standard ; Standard intact (axe 0 violation) |
| 11 | **Thèmes** prédéfinis (§5.3) : Ardoise + au moins un autre, clair/sombre chacun, réglage Auto/Clair/Sombre, choix par profil et par écran | Un écran en thème B sombre forcé ; contrastes de chaque thème dans REPORT.md ; axe 0 violation par thème |

### Lot 1 — fait (2026-10-05)

- `backend/` : `registry.go` (mDNS, cache des TD, relecture 5 min, hors ligne après 35 s),
  `services.go` (détection par signature sur le catalogue eria-core + `Contexts`, sous-ensembles
  éliminés, libellé, lecture seule forcée hors `on`/`brightness`/`position`), `main.go`
  (`GET /api/services`, proxy GET/PUT/POST passant par `forward`, JSON validé, 64 Ko max).
- Masquage par motifs dans `config.yml` (`hide`) : App Controllers, agents, orchestrateur.
- Tests : `go test` sur 8 TD réels (`testdata/`).
- Banc : tourne à la main sur le Pi (`~/ui-user`, port 8090, `nohup`) → 52 services, 0 hors ligne.
- Non testé : les écritures réelles (PUT/POST) vers un Thing — maison en production.
- Limites connues : propriétés à `uriVariables` non relayées (501) ; TD lus un par un.

### Lot 2 — fait (2026-10-05)

- `hub.go` : `GET /api/ws` ; une WebSocket upstream par (Thing, propriété), partagée et
  comptée ; fermée au départ du dernier abonné ; reconnexion avec attente 1 s → 30 s (TD relu
  à chaque essai) ; valeur courante lue en HTTP à la connexion (go-wot n'envoie que les
  changements) et gardée en cache pour les abonnés tardifs ; ping/pong 30 s / 70 s des deux
  côtés ; `{thing, online}` et `{op:"things-changed"}` diffusés (scrutation du registre 5 s).
- Garde-fous : origine vérifiée (anti-CSWSH, défaut gorilla), message client ≤ 4 Ko,
  ≤ 500 observations par client, client trop lent déconnecté (jamais de blocage du hub).
- Test `hub_test.go` (faux Thing) : 2 clients → 1 socket amont, valeur initiale + cache,
  fermeture au dernier départ ; passe avec `-race`. Vérifié sur le banc avec la maison réelle.
- Dépendance ajoutée : `github.com/gorilla/websocket` v1.5.3 (déjà présente via go-wot).

### Lot 3 — fait (2026-10-05)

- `frontend/` : SvelteKit 3 (config passée à `sveltekit({ adapter })` dans `vite.config.ts` —
  plus de `svelte.config.js` —, alias `#lib/…` par `imports` de package.json, extensions
  explicites), SPA `adapter-static` écrite dans `../backend/web`, embarquée par `go:embed` et
  servie par le backend (repli sur `index.html`). Workspace pnpm propre (`pnpm-workspace.yaml`
  local : sinon pnpm rattache le paquet au workspace racine v3).
- Client `#lib/client/eria.ts` : poignées de Thing stables (pas de réabonnement au rendu),
  observations partagées et mises en cache côté navigateur, renvoyées après reconnexion.
- Widgets Lit génériques : `eria-switch` (APG switch), `eria-number` (curseur si borné, envoi
  au relâchement), `eria-value`, `eria-button` ; erreurs → évènement `eria-error` → alerte.
- Coquille : lien d'évitement, barre du bas (téléphone) / rail (≥ 48rem), alertes et
  « reconnexion » en régions live, Paraglide FR/EN.
- Identité : ardoise jour/nuit, ambre réservé à « allumé », Atkinson Hyperlegible Next
  embarquée. Contrastes calculés : texte ≥ 5,6:1, bordures ≥ 3,5:1.
- Tests : Vitest (client : partage, cache, reconnexion, encodage ; formatage).
- Limites : libellés génériques = titres anglais des TD (« On/Off », « Toggle ») → lot 4 ;
  pas encore d'axe-core automatisé.
- Essai sur `lampeCAV` (accord de Cédric) par écriture de `on` : chaîne logicielle validée,
  mais **la lampe n'a pas été pilotée** (écriture = cache seulement, découvert au lot 4).

### Lot 4 — fait (2026-10-05)

- Widgets de capacité Lit (`widgets/capabilities.ts`) : `eria-light` (interrupteur APG via
  `toggle`, variateur via `fade`), `eria-shutter` (Ouvrir / Arrêter / Fermer, ouverture via
  `setPosition` ; position = % d'ouverture), `eria-garage` (confirmation par défaut, focus sur
  Annuler), `eria-trigger`, `eria-sensor` (toutes les mesures, direction du vent en mots).
- Variantes `tile` / `full` / `value` et tailles permises déclarées par capacité
  (`widgets/index.ts`, `catalog`) ; 1 × 1 réservé aux valeurs courtes ; mise en page par
  container queries (resserrée ≤ 12rem, empilée ≤ 9rem) ; grandes valeurs réduites selon leur
  longueur, unité plus petite.
- Dialogues natifs `<dialog>` (Détails, confirmation) : modaux, nommés, Échap, retour du focus
  vérifiés au clavier.
- Page `/demo` (hors navigation) : chaque widget × variante × taille, services réels ;
  45 cases, aucune ne déborde.
- axe-core 4.11 (WCAG 2.2 AA + best practices) : 0 violation sur `/demo` et `/`, clair et
  sombre. 6 « incomplets » contraste dans les cases 1 × 1 (fond non calculable) : couleurs =
  jetons mesurés (≥ 7:1).
- Backend : propriétés de capacité toutes en lecture seule.
- Pilotage réel validé : `lampeCAV` allumée (Espace) puis éteinte (clic) via `toggle` ; l'état
  revient du bus KNX (lu sur le Thing). Porte de garage : confirmation ouverte puis annulée,
  porte restée fermée.
- Reste : `fade` sans schéma d'entrée dans le TD (envoyé : nombre 0-100, non essayé en réel) ;
  `setPosition` non essayé en réel.

### Lot 5 — fait (2026-10-05)

- Modes : manager-contexts ne publie pas de liste ; `modes` (id + libellé) dans `config.yml`
  du backend, `GET /api/modes` ; widget `eria-modes` (interrupteurs APG, `setContext` /
  `unsetContext`) qui montre aussi tout contexte actif hors liste (par son id), jamais les
  contextes du jour (lundi…, semaine / week-end). Liste par défaut : Absent, Invités,
  Canicule, Vacances.
- Sections (`#lib/sections.ts`, `#lib/Section.svelte`) : Modes, Lumières (tuiles), Ouvrants
  (tuiles), Climat (valeurs) ; grille de cartes commune (≥ 4 colonnes sur téléphone, lignes
  `minmax(5rem, auto)` qui grandissent si besoin, ordre visuel = ordre de lecture).
- Écrans prédéfinis : `/` Accueil (Modes + Climat), `/lights`, `/openings`, `/climate`,
  `/modes`, `/services` (« Tous ») ; titre de page unique par écran.
- Corrections : hub — un Thing pas encore découvert (backend redémarré) est accepté et suivi
  dès qu'il apparaît (test ajouté) ; transition limitée au halo (le fond et le texte changent
  ensemble : contraste jamais cassé) ; état sous le nom dans les cases ≤ 10,5rem.
- axe-core : 0 violation, 0 débordement sur les 6 écrans.
- Validé sur la maison (accord de Cédric) : « Invités » activé au clavier depuis l'Accueil puis
  désactivé à la souris ; état lu directement sur manager-contexts.
- Limites : barre de navigation téléphone en défilement horizontal (6 entrées texte, pas
  d'icônes) ; libellés « Weather » des mesures météo (renommage : lot 7).

### Lot 6 — fait (2026-10-06)

- Backend `auth.go` : SQLite (`modernc.org/sqlite`, Go pur, fichier `database:` de la config),
  tables `profiles` / `sessions`. PIN 4 à 8 chiffres haché argon2id (m=19 Mio, t=2, p=1).
  Verrouillage par profil après 5 erreurs : 1 min, puis double à chaque erreur (max 1 h),
  journalisé ; réponse 429 + `Retry-After`. Session : jeton aléatoire 256 bits (haché en base),
  cookie HttpOnly SameSite=Strict, 30 jours ; nouveau PIN ou suppression → sessions closes.
- `Protect` : tout `/api` exige une session sauf `GET /api/auth/profiles` et
  `POST /api/auth/login` ; invité = lecture seule (403 sur les commandes) ; `/api/profiles`
  réservé aux admins ; dernier admin ni supprimé ni rétrogradé. Anti-CSRF : toute écriture
  doit être en `application/json` (415 sinon) + SameSite=Strict.
- Premier admin : `UI_USER_ADMIN_PIN` (environnement, jamais en fichier de config ni en
  argument). Banc : PIN généré dans `~/ui-user/admin-pin` (600), à changer.
- Frontend : écran « Qui êtes-vous ? » (profils en boutons radio, PIN numérique, erreur et
  verrouillage annoncés et reliés au champ, focus rendu au champ) ; « Changer de profil »
  (rail ou haut de page sur téléphone) ; page **Profils** (admin) : ajout, nom, rôle, nouveau
  PIN, suppression ; widgets en lecture seule pour les invités, états restés lisibles.
- Pièges notés : Svelte met en minuscules les noms passés par spread aux éléments
  personnalisés, et prend `readonly` pour l'attribut HTML → propriété `viewonly`.
- Tests : `auth_test.go` (SQLite réelle : rôles, CSRF, verrouillage et déblocage, dernier
  admin, PIN changé, déconnexion). Essai navigateur en local (`localhost`, base `dev.db`,
  PIN de test dans `backend/dev.env`, ignoré par git) : connexion, erreur, verrouillage,
  invité, gestion des profils ; axe 0 violation (connexion, Profils, Lumières en invité).

### Lot 7 — fait (2026-10-06)

- Backend `screens.go` : table `screens` (owner NULL = partagé, `edit_role`, `position`,
  `version`, `layout` JSON) dans la même base ; `GET/POST /api/screens`,
  `PUT/DELETE /api/screens/{id}`. Partagé : créé et supprimé par un admin, modifiable à partir de
  `editRole` (admin par défaut, ouvrable aux membres ; seul un admin le change) ; personnel :
  invisible et intouchable par les autres (404). Enregistrement atomique d'un brouillon avec
  sa `version` : 409 si quelqu'un a enregistré entre-temps. Forme des cartes vérifiée
  (type, id, taille ≤ 4 × 8, ≤ 200 cartes) ; les services cités ne le sont pas (peuvent être
  hors ligne). Profil supprimé → ses écrans aussi. Tests : `screens_test.go`.
- Frontend : page **Écrans** (liste partagés / miens, création ; « partagé » réservé aux
  admins) ; écrans dans le menu après les prédéfinis ; `/screens/{id}` : grille commune
  (`.card-grid`), cartes service (widget avec réglages de la carte), section (bloc prédéfini
  pleine largeur), titre (`h2`) ; service absent → carte « Indisponible ».
- Mode modification : bouton « Modifier la page » si autorisé ; focus sur la barre de
  modification, annonce ; widgets `inert` ; par carte : Réglages (dialogue natif : affichage,
  taille permise, nom sur la carte, valeurs affichées, confirmation), Monter / Descendre
  (`aria-disabled` aux extrémités, le focus reste sur le bouton), Retirer ; Ajouter une carte
  (service par capacité, section, titre) ; titre, position dans le menu, rôle requis (admin) ;
  Enregistrer / Annuler (focus rendu) ; conflit expliqué avec « Recharger la page » ; garde
  avant de quitter avec un brouillon (`beforeNavigate`).
- Vérifié en local (admin de test) : création, 4 cartes ajoutées, réglage, déplacement,
  enregistrement (version 2), conflit, garde de sortie, annulation ; axe 0 violation en
  affichage, en modification et dialogue ouvert ; aucun débordement mesuré. Capture d'écran
  non faite (volet navigateur masqué).
- Limites : carte titre haute d'une ligne de grille (5rem) ; menu de plus en plus long sur
  téléphone (défilement horizontal).

### Lot 8 — prêt (2026-10-06), déploiement à faire par Cédric

- Décision : déployer par le maître (catalogue, DESIGN §12) plutôt qu'un service systemd à
  la main ; publication par **dépôt GitHub + CI** ; nœud choisi au déploiement. Constat : le
  maître de production gère Core1, Core3 et garage — pas le Pi du banc (agent non rattaché).
- `eria-app.yml` (kind `service`, base dans `{data}/ui-user.db` via `UI_USER_DATABASE`, santé
  `GET /api/health` publique, modèle de config sans secret avec `adminPin` à remplir).
- Backend : `adminPin` dans la config (canal à secrets du déploiement, utilisé seulement sans
  profil), `UI_USER_DATABASE`, `/api/health`. Module `github.com/project-eria/ui-user`.
- CI propre au dépôt (`.github/workflows/release.yml` + `.goreleaser.yaml`) : la CI partagée
  attend un module Go à la racine et pas de frontend. Tests front et back, puis GoReleaser :
  mêmes assets que les autres apps (binaires bruts, `eria-app.yml`, `checksums.txt`).
  `.gitignore` racine (build `backend/web/` jamais commité, `.gitkeep` gardé pour `go:embed`).
- Vérifié : release à blanc (`goreleaser --snapshot`) sur une copie git temporaire ; binaire
  arm64 lancé sur le Pi comme par l'agent (dossier de données, config avec `adminPin`) :
  santé 204, page 200, admin créé, version injectée.
- À faire (Cédric) : créer `project-eria/ui-user`, y pousser `src/ui-user`, taguer `v0.1.0`,
  « Rafraîchir » le catalogue, déployer en **épinglant un nœud**. Si c'est le Pi : arrêter
  d'abord l'instance lancée à la main (port 8090) et rattacher son agent au maître.

### Lot 9 — fait (2026-10-06), statut CONDITIONAL

- `REPORT.md` et `A11Y-DECISIONS.md` créés depuis les gabarits A11Y.md 2.2.0 (pas
  d'`EXCEPTIONS.md` : aucun écart accepté). Profil Standard (AA), cibles 44 px.
- Contrôles : axe-core 4.11 (+ `label-content-name-mismatch`, règles expérimentales) 0 violation
  sur 9 écrans, clair et sombre, mode modification et dialogues ; reflow 320 px, texte 200 %,
  espacement du texte (SC 1.4.12, jusque dans les shadow roots) ; contrastes recalculés avec
  `contrast-check.py` (26 paires) ; contrôle statique `verify-a11y.py` PASS (Python 3.13 via
  nix ; à lancer **après** le build : le build régénère `src/lib/paraglide`).
- Vérification **fresh-context** par un sous-agent neuf : a trouvé 2 vrais défauts (focus perdu
  entre dialogues, 2e Échap perdant des réglages) + 3 mineurs ; corrigés et revérifiés.
- Autres corrections : réglages non appliqués protégés à l'Échap ; noms jamais tronqués ;
  valeurs longues coupées ; rail défilant ; focus initial des Détails sur le titre ; état
  inconnu non opérable ; régions live toujours présentes ; soulignement de page courante en
  encre (l'ambre ne veut dire qu'« allumé ») ; `outline: none` retiré.
- **Reste (Cédric)** : VoiceOver + Safari (macOS, iOS), Contrôle vocal, couleurs forcées —
  scénarios listés dans REPORT.md §3. Statut PASS seulement après.

### Lot 10 — fait (2026-10-06)

- Backend `packs.go` : lecture des dossiers `packs/<id>/` à chaque requête (pas de
  redémarrage), validation (`id` = dossier, `sdk: 1`, balises préfixées `<id>-`, `entry` et
  `report` dans le dossier, tailles 1-4 × 1-8) ; un pack invalide est listé avec sa raison ;
  `GET /api/packs` (session), fichiers sous `/packs/` (`os.DirFS` : pas de sortie du dossier,
  `nosniff`, `no-cache`). Pack par profil (`PUT /api/me`, tous rôles) et par écran ; colonnes
  ajoutées par migration. `packs:` / `UI_USER_PACKS` (`{data}/packs` via l'orchestrateur).
- Frontend : SDK `/sdk/eria-widgets.js` (build Vite séparé, URL stable : Lit, `EriaWidget`,
  widgets Standard à étendre, formatage, textes localisés) ; résolution pure
  `widgets/catalog.ts` (`resolve` : pack, sinon Standard ; la confirmation reste celle de
  Standard) ; chargement à l'exécution (`#lib/packs.ts`, `import()` dynamique, balises
  vérifiées) ; Standard affiché en attendant et en cas d'échec, avec message. Page
  **Préférences** (choix du pack ; admins : packs installés et leur état) ; choix du pack dans
  la barre de modification d'un écran.
- Contrat de variables `--eria-*` élargi (police, marges, écart, focus, halo) : base des thèmes.
- Exemple `packs-examples/contour/` (lampes en bouton rond cerclé, JS pur, sans build) ;
  contrat documenté dans `PACKS.md`.
- Vérifié : tests Go (validation, sortie du dossier refusée, préférence) et Vitest (résolution,
  repli) ; en local : Contour choisi → 29 lampes en `contour-light`, volets en Standard ; écran
  forcé en Standard ; pack cassé → Standard + message ; axe 0 violation sur 7 écrans ; gate PASS.
- Limites : SDK non minifié (Vite ne minifie pas le format ES en bibliothèque ; 21 ko gzip) ;
  Lit présent deux fois (app + SDK) ; tailles d'une carte gardées si on change de pack ;
  pas de téléversement depuis l'UI (dépôt de fichiers) ; les packs ne redessinent pas les
  sections. Dossier de test `packs-examples/casse/` à supprimer (suppression bloquée).

### Lot 11 — fait (2026-10-06)

- Thèmes livrés : **Ardoise** (défaut, AA) et **Contraste** (« Contraste élevé » : texte
  ≥ 7:1, focus 4 px, sans halo, angles plus nets), chacun clair et sombre. Valeurs des
  variables `--eria-*` dans `app.css`, choisies par `<html data-theme data-scheme>` ;
  `data-scheme` : auto (système) / light / dark.
- Choix par profil (Préférences : thème, clair/sombre, pack) et par écran (barre de
  modification) ; `PUT /api/me` ne change que les champs envoyés ; validation côté backend
  (`themes.go`) ; colonnes `theme`, `scheme` ajoutées par migration. Le thème d'un écran
  s'applique à toute la page pendant qu'il est affiché.
- Dernier thème mémorisé dans le navigateur et appliqué avant le premier affichage (pas de
  flash d'un autre thème au chargement).
- Vérifié : tests Go et Vitest ; parcours réel (Préférences → Contraste sombre ; écran
  « Salon » forcé Ardoise clair ; retour au choix du profil) ; axe 0 violation sur 6 écrans ×
  4 combinaisons ; 20 paires de contraste ajoutées à REPORT.md, recalculées par le gate (PASS).
- Limite : pas d'aperçu du thème pendant la modification d'un écran (appliqué à
  l'enregistrement).

## 9. Hors périmètre / dépendances externes

- eria-core : publier `@type` et `readOnly` dans les TD (simplifie la détection).
- Service d'autorisation + vérification des jetons dans go-wot / rs-wot.
- Pièces comme notion de données (au-delà des écrans personnalisés).
- Déploiement en production via le catalogue eria.
- Historique / graphes, PWA, événements WoT : non retenus en v1.

## 10. Questions ouvertes

Aucune pour l'instant.

Décidé le 2026-10-05 : profil A11y Standard (AA) ; déploiement sur la machine de test (Pi du
banc, port **8090**) ; on affiche les services, jamais les apps ; écrans prédéfinis (Accueil,
Lumières, Ouvrants, Climat, Modes) et personnalisés (partagés + personnels) dans une même
navigation.
