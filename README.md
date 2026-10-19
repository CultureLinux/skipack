![skipack — Pack skills. Ship focus.](assets/skipack-banner.png)

# skipack

Installez des groupes de skills pour vos agents de développement, directement
depuis GitHub.

`skipack` est une CLI Go multiplateforme qui copie des skills publics dans
`.agents/skills/`. Elle fonctionne sans Git, Node.js ou Python et ne lance
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

Sous Windows amd64, téléchargez `skipack-windows-amd64.exe` depuis la
[dernière release](https://github.com/cyklodev/skipack/releases/latest),
renommez-le éventuellement en `skipack.exe`, puis placez-le dans un dossier
présent dans votre `PATH`.

Sous Linux amd64 :

```console
mkdir -p ~/.local/bin
curl -fL https://github.com/cyklodev/skipack/releases/latest/download/skipack-linux-amd64 -o ~/.local/bin/skipack
chmod +x ~/.local/bin/skipack
skipack help
```

Remplacez `amd64` par `arm64` sur un système Linux ARM64. Les utilisateurs
macOS peuvent télécharger `skipack-darwin-amd64` ou
`skipack-darwin-arm64` selon leur processeur, puis appliquer `chmod +x`.

### Installation avec Go

Avec Go 1.23 ou plus récent :

```console
go install github.com/cyklodev/skipack/cmd/skipack@latest
```

Vous pouvez également construire le projet localement :

```console
git clone https://github.com/cyklodev/skipack.git
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
| `skipack add <groupe>` | Installe tous les skills du groupe. |
| `skipack delete <groupe>` | Supprime uniquement les skills suivis de ce groupe. |
| `skipack update` | Met à jour uniquement les skills suivis dans le projet. |
| `skipack help` | Affiche l’aide et des exemples. |

`add` ignore tout dossier déjà présent sans le modifier. `update` prépare et
valide tous les téléchargements avant de remplacer les versions existantes.
`delete` ne touche jamais aux dossiers qui ne sont pas suivis dans
`.agents/skipack-state.json`.

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

## Sécurité

Les sources distantes sont limitées aux dépôts GitHub publics accessibles en
HTTPS. Lors d’une installation, `skipack` :

- refuse les redirections vers un hôte inattendu ;
- limite la taille des téléchargements et applique un délai maximal ;
- rejette les chemins absolus, traversées de répertoires, symlinks et entrées
  spéciales présents dans une archive ;
- exige un fichier `SKILL.md` à la racine du skill ;
- prépare les fichiers dans un dossier temporaire ;
- n’exécute aucun fichier du skill téléchargé.

Le contenu d’un skill reste du code non fiable : relisez toujours ses
instructions avant de l’utiliser avec un agent.

## Développement

```console
go test ./...
go vet ./...
go build ./cmd/skipack
```

Le projet normalise les fichiers texte en UTF-8 et leurs fins de ligne avec
`.editorconfig` et `.gitattributes` pour conserver un comportement cohérent
sous Windows, Linux et macOS.

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
