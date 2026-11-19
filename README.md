![skipack — Pack skills. Ship focus.](assets/skipack-banner.png)

# skipack

Installez des groupes de skills pour vos agents de développement, directement
depuis GitHub.

`skipack` est une CLI Go multiplateforme qui copie des skills publics dans
`.agents/skills/` (Codex) ou `.claude/skills/` (Claude Code), ou les deux. Elle
fonctionne sans Git, Node.js ou Python et ne lance
jamais les scripts téléchargés.

> **Statut :** préversion. Le format de configuration V1 est stable.

## Pourquoi skipack ?

- regrouper les skills utilisés ensemble sous un nom simple ;
- partager une même sélection entre plusieurs projets ;
- installer depuis un dépôt GitHub public sans cloner le dépôt entier ;
- mettre à jour uniquement les dossiers installés et suivis par `skipack` ;
- conserver les skills ajoutés manuellement sans jamais les modifier.

## Installation

### Binaires GitHub Releases

Chaque tag `v*` publie des binaires autonomes pour Windows, Linux et macOS,
accompagnés d’un fichier `SHA256SUMS`.

### Windows amd64 (PowerShell)

Téléchargez la dernière version de `skipack`, renommez l'exécutable en `skipack.exe` et installez-le dans `%USERPROFILE%\bin`. Le répertoire sera automatiquement ajouté au `PATH` utilisateur s'il n'y figure pas déjà.

```powershell
# Créer le répertoire d'installation
New-Item -ItemType Directory -Force "$HOME\bin" | Out-Null

# Télécharger la dernière release
Invoke-WebRequest `
    -Uri "https://github.com/CultureLinux/skipack/releases/download/v0.1.7/skipack-windows-amd64.exe" `
    -OutFile "$HOME\bin\skipack.exe"

# Ajouter le répertoire au PATH utilisateur si nécessaire
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")

if (($userPath -split ';') -notcontains "$HOME\bin") {
    $newPath = (@($userPath, "$HOME\bin") | Where-Object { $_ }) -join ';'
    [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
}

# Actualiser le PATH de la session courante
if (($env:PATH -split ';') -notcontains "$HOME\bin") {
    $env:PATH += ";$HOME\bin"
}

# Vérifier l'installation
skipack help
```

La commande `skipack` sera ensuite disponible depuis PowerShell, CMD et les terminaux VS Code nouvellement ouverts, sans droits administrateur.

Sous Linux amd64 :

```console
mkdir -p ~/.local/bin
curl -fL https://github.com/CultureLinux/skipack/releases/latest/download/skipack-linux-amd64 -o ~/.local/bin/skipack
chmod +x ~/.local/bin/skipack
skipack help
```

Remplacez `amd64` par `arm64` sur un système Linux ARM64. Les utilisateurs
macOS peuvent télécharger `skipack-darwin-amd64` ou
`skipack-darwin-arm64` selon leur processeur, puis appliquer `chmod +x`.

### Installation avec Go

Avec Go 1.23 ou plus récent :

```console
go install github.com/CultureLinux/skipack/cmd/skipack@latest
```

Vous pouvez également construire le projet localement :

```console
git clone https://github.com/CultureLinux/skipack.git
cd skipack
go build -o skipack ./cmd/skipack
```

## Démarrage rapide

Créez un fichier YAML encodé en UTF-8 :

```yaml
version: 1
groups:
  reflex:
    - source: owner/repository
      skill: investigate
    - source: owner/repository
      skill: caveman
  frontend:
    - source: https://github.com/owner/repository/tree/main/skills/frontend-design
```

Enregistrez la configuration, inspectez son contenu puis installez un groupe :

```console
skipack config set ./skipack.yaml
skipack list
skipack add reflex
# Ou installer tous les skills de tous les groupes
skipack add --all
```

Les skills sont installés relativement au dossier courant :

```text
.agents/
├── skills/
│   ├── investigate/
│   │   └── SKILL.md
│   └── caveman/
│       └── SKILL.md
└── skipack-state.json
```

## Utiliser une configuration hébergée sur GitHub

1. Créez un fichier `skipack.yaml` dans un dépôt GitHub public.
2. Ajoutez-y vos groupes de skills, puis validez-le sur votre branche par
   défaut.
3. Ouvrez le fichier sur GitHub et copiez son URL `blob`, par exemple :

```text
https://github.com/owner/my-skipack-config/blob/main/skipack.yaml
```

4. Enregistrez cette URL une seule fois sur votre machine :

```console
skipack config set https://github.com/owner/my-skipack-config/blob/main/skipack.yaml
skipack config show
skipack list
```

`skipack` relit le fichier GitHub à chaque `list` ou `add` : les changements
validés dans le dépôt deviennent donc disponibles sans reconfigurer la CLI.
Les branches et tags contenant `/` sont résolus via l’API GitHub.

La source active est conservée dans le répertoire renvoyé par
`os.UserConfigDir` : `%AppData%\skipack` sous Windows,
`~/.config/skipack` sous Linux et `~/Library/Application Support/skipack` sous
macOS. Une configuration locale est également acceptée ; son chemin absolu est
alors mémorisé.

## Commandes

| Commande | Description |
| --- | --- |
| `skipack config set <source>` | Enregistre un fichier local ou une URL GitHub publique. |
| `skipack config show` | Affiche la source active. |
| `skipack list` | Liste les groupes et leurs skills. |
| `skipack audit` | Vérifie les sources et les fichiers `SKILL.md` sans installer de skill. |
| `skipack add <groupe>...` | Installe tous les skills d’un ou plusieurs groupes. |
| `skipack add --all` | Installe tous les skills de tous les groupes configurés. |
| `skipack agent set <agent>...` | Choisit `codex` (`.agents/skills`), `claude` (`.claude/skills`) ou les deux. Les skills suivis sont copiés localement, sans téléchargement. |
| `skipack agent show` | Affiche les agents du projet (défaut : `codex`). |
| `skipack delete <groupe>...` | Supprime uniquement les skills suivis d’un ou plusieurs groupes. |
| `skipack delete --all` | Supprime tous les skills suivis dans le projet. |
| `skipack update [--dry-run]` | Rafraîchit le cache GitHub puis met à jour les skills suivis ; `--dry-run` liste les changements sans les appliquer. |
| `skipack self-update` | Télécharge et installe la dernière release de skipack. |
| `skipack version` | Affiche la version du binaire installé. |
| `skipack help` | Affiche l’aide et des exemples. |

`add` ignore tout dossier déjà présent sans le modifier. `update` prépare et
valide tous les téléchargements avant de remplacer les versions existantes. Si
une ou plusieurs sources de skills sont inaccessibles, il les liste toutes,
renvoie un code d’erreur et conserve les versions déjà installées.
`delete` ne touche jamais aux dossiers qui ne sont pas suivis dans
`.agents/skipack-state.json`.

Pour changer complètement de contexte sans supprimer les skills ajoutés
manuellement :

```console
skipack delete --all
skipack add autre-contexte
```

## Codex et Claude

Par défaut, les skills sont installés pour Codex dans `.agents/skills`. Claude
Code lit `.claude/skills` :

```console
skipack agent set codex claude   # les deux cohabitent
skipack agent set claude         # passe à Claude seul
skipack agent show
```

Changer d'agent ne retélécharge rien : les skills suivis sont copiés depuis le
dossier existant, puis `add`, `update` et `delete` s'appliquent à chaque dossier.
Seuls les sous-dossiers `skills/` sont gérés : `.claude/settings.json` et le
reste de `.claude/` ne sont jamais touchés. Les skills ajoutés à la main restent
en place, et un dossier de destination déjà présent interrompt la commande avant
toute modification. L'état reste dans `.agents/skipack-state.json`.

## Cache GitHub

Pour limiter les appels à l’API GitHub et éviter les erreurs de rate limit
`403`, skipack met en cache les métadonnées, les refs, les configurations
distantes et les archives téléchargées. Plusieurs skills provenant du même
dépôt réutilisent donc la même archive, y compris lors d’exécutions séparées.

Le cache est stocké à côté du fichier de settings :

- Windows : `%AppData%\skipack\cache\`
- Linux : `~/.config/skipack/cache/`
- macOS : `~/Library/Application Support/skipack/cache/`

`list` et `add` privilégient le cache existant (valable 24 h). La commande suivante force un
rafraîchissement réseau des ressources utiles, une seule fois par URL, puis
met à jour les skills suivis dans le projet :

```console
skipack update
```

Si GitHub refuse ou interrompt le rafraîchissement, l’ancien cache et les
skills déjà installés sont conservés.

Les réponses en cache sont réutilisées 24 heures, puis redemandées à GitHub (le
cache périmé sert de secours si le réseau échoue). Les fichiers de plus de 30
jours sont supprimés et le cache est limité à 500 Mo. Voir la section suivante
pour augmenter la limite de requêtes de GitHub.

## Jeton GitHub

Sans authentification, l’API GitHub accepte 60 requêtes par heure et par adresse
IP. Une configuration avec beaucoup de skills peut atteindre cette limite
(message `GitHub API rate limit reached`, avec l’heure de remise à zéro). Avec un
jeton, la limite passe à 5000 requêtes par heure.

`skipack` lit le jeton dans **une variable d’environnement du processus** :

1. `GITHUB_TOKEN`, sinon
2. `GH_TOKEN`.

Il ne lit **pas** de fichier `.env`, ni dans le projet ni ailleurs. Mettre le
jeton dans un `.env` n’a donc aucun effet sur skipack (et ne le commitez jamais).
Aucune permission n’est nécessaire pour lire des dépôts publics : un jeton sans
scope (classique) ou sans accès particulier (fine-grained) suffit.

### Obtenir un jeton

Deux solutions, au choix.

**1. Créer un jeton sur GitHub** (compte GitHub requis) :

1. Connectez-vous à GitHub, puis ouvrez
   <https://github.com/settings/personal-access-tokens/new>
   (*Settings → Developer settings → Personal access tokens → Fine-grained tokens
   → Generate new token*).
2. Donnez-lui un nom (par exemple `skipack`) et une date d’expiration (30 à 90
   jours est raisonnable).
3. Dans *Repository access*, choisissez **Public repositories (read-only)**.
4. Ne cochez **aucune permission** : skipack ne lit que des dépôts publics.
5. Cliquez sur *Generate token* et copiez la valeur affichée (elle commence par
   `github_pat_`). GitHub ne la montre qu’une seule fois : si vous la perdez,
   générez-en un autre.

Un jeton classique fonctionne aussi
(<https://github.com/settings/tokens>, *Generate new token (classic)*, sans cocher
aucun scope ; la valeur commence par `ghp_`), mais le jeton fine-grained est
préférable car il ne donne accès à rien de privé. Les intitulés de l’interface
GitHub peuvent évoluer.

**2. Réutiliser GitHub CLI** si `gh` est installé : après `gh auth login`,
`gh auth token` affiche le jeton de votre session. Il a généralement plus de
droits que nécessaire : préférez un jeton dédié si vous le conservez.

Traitez le jeton comme un mot de passe : ne le collez pas dans un fichier
versionné, un ticket ou une capture d’écran. S’il fuite, révoquez-le dans les
réglages GitHub et créez-en un nouveau.

### Fournir le jeton à skipack

Pour la session courante :

```powershell
# PowerShell
$env:GITHUB_TOKEN = "ghp_xxx"
# ou, si GitHub CLI est installé et connecté :
$env:GITHUB_TOKEN = gh auth token
```

```console
# Linux et macOS
export GITHUB_TOKEN=ghp_xxx
export GITHUB_TOKEN="$(gh auth token)"
```

Pour le conserver, définissez-le comme variable d’environnement utilisateur :
`setx GITHUB_TOKEN "ghp_xxx"` sous Windows (visible dans les nouveaux terminaux
seulement), ou ajoutez l’`export` à `~/.profile`, `~/.zshrc` ou équivalent. Dans
GitHub Actions, transmettez `${{ secrets.GITHUB_TOKEN }}` via `env:`.

Fonctionnement et garanties :

- le jeton n’est envoyé que dans l’en-tête `Authorization` des requêtes vers
  `api.github.com` ; il n’est jamais envoyé à un autre hôte, y compris lors des
  redirections vers `codeload.github.com` ;
- il reste en mémoire pour la durée de la commande : il n’est écrit ni dans le
  cache, ni dans `.agents/skipack-state.json`, ni dans la configuration, ni dans
  les messages d’erreur ;
- les dépôts privés restent hors périmètre, même avec un jeton : le jeton ne sert
  qu’à relever la limite de requêtes ;
- si le jeton est invalide ou expiré, GitHub répond en général `401` et skipack s’arrête
  avec ce statut : retirez la variable ou renouvelez le jeton.

## Mise à jour de skipack

La mise à jour des skills et celle du binaire sont séparées :

```console
skipack self-update
```

La commande télécharge le binaire correspondant au système et à l’architecture
courants depuis la dernière GitHub Release, puis vérifie sa somme SHA-256 avec
`SHA256SUMS` avant de le remplacer. Elle prend en charge Windows amd64, Linux
amd64/arm64 et macOS amd64/arm64. Sous Windows, le remplacement se termine juste
après la fermeture de la commande, car un exécutable en cours d’utilisation ne
peut pas être remplacé directement.

Sous Windows, un exécutable temporaire à nom unique termine le remplacement puis
se supprime ; en cas d’échec, l’erreur est écrite dans `skipack\self-update.log`
(dossier renvoyé par `os.UserConfigDir`). La somme SHA-256 provient de la même
release que le binaire : elle protège d’un téléchargement corrompu, pas d’une
release compromise.

## Sources de skills

Une URL complète désigne directement un dossier :

```yaml
- source: https://github.com/owner/repository/tree/main/path/to/my-skill
```

Une référence courte recherche un skill dans quatre emplacements :

```yaml
- source: owner/repository
  skill: my-skill
```

Emplacements pris en charge :

1. `skills/my-skill`
2. `.agents/skills/my-skill`
3. `.claude/skills/my-skill`
4. `my-skill`

Si plusieurs emplacements existent, l’installation s’arrête avec une erreur
d’ambiguïté au lieu d’en choisir un arbitrairement.

### Épingler une version

Une référence courte peut être figée sur une branche, un tag ou un commit avec
`ref` :

```yaml
- source: owner/repository
  skill: my-skill
  ref: v1.2.0
```

Avec une URL complète, la référence fait déjà partie de l’URL (`tree/<ref>/…`).
`skipack` enregistre le commit installé dans `.agents/skipack-state.json`.
`skipack update --dry-run` liste ce qui changerait (`would update x (ancien ->
nouveau)`) sans rien modifier ; `skipack update` affiche les mêmes lignes en
appliquant les changements.

## Sécurité

Les sources distantes sont limitées aux dépôts GitHub publics accessibles en
HTTPS. Lors d’une installation, `skipack` :

- refuse les redirections vers un hôte inattendu ;
- limite la taille des téléchargements et applique un délai maximal ;
- rejette les chemins absolus, traversées de répertoires, symlinks et entrées
  spéciales présents dans une archive ;
- exige un fichier `SKILL.md` à la racine du skill ;
- prépare les fichiers dans un dossier temporaire ;
- rejette aussi les noms de fichiers spéciaux sous Windows (`:`, `CON`, `NUL`,
  points ou espaces finaux), sur tous les systèmes ;
- limite à 50 Mo la taille extraite du skill (et à 200 Mo l’archive du dépôt) ;
- refuse deux exécutions simultanées dans un même projet (`.agents/skipack.lock`)
  et répare au démarrage les restes d’une exécution interrompue ;
- n’exécute aucun fichier du skill téléchargé.

Le contenu d’un skill reste du code non fiable : relisez toujours ses
instructions avant de l’utiliser avec un agent.

## Développement

```console
gofmt -l .
go test ./...
go vet ./...
go build ./cmd/skipack
```

Le projet normalise les fichiers texte en UTF-8 et leurs fins de ligne avec
`.editorconfig` et `.gitattributes` pour conserver un comportement cohérent
sous Windows, Linux et macOS. Le workflow `CI` rejoue `gofmt`, `go vet` et
`go test` sur chaque push vers `main` et chaque pull request, avec `-race` sur
Linux.

## Publication d’une release

Le workflow GitHub Actions teste le projet sous Windows, Linux et macOS. Un tag
`v*` déclenche ensuite la compilation des cinq binaires et crée la release :

```console
git tag v0.1.0
git push origin v0.1.0
```

Les artefacts publiés couvrent Windows amd64, Linux amd64/arm64 et macOS
amd64/arm64.

Les dépôts privés, la recherche sur skills.sh, les interfaces graphiques et la
gestion des dépendances entre skills ne font pas partie de la V1.
