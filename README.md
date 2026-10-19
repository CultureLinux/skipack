# skipack

Installez des groupes de skills pour vos agents de développement, directement
depuis GitHub.

`skipack` est une CLI Go multiplateforme qui copie des skills publics dans
`.agents/skills/`. Elle fonctionne sans Git, Node.js ou Python et ne lance
jamais les scripts téléchargés.

> **Statut :** préversion. Le format de configuration V1 est stable, mais les
> binaires GitHub Releases arriveront en V2.

## Pourquoi skipack ?

- regrouper les skills utilisés ensemble sous un nom simple ;
- partager une même sélection entre plusieurs projets ;
- installer depuis un dépôt GitHub public sans cloner le dépôt entier ;
- mettre à jour uniquement les dossiers installés et suivis par `skipack` ;
- conserver les skills ajoutés manuellement sans jamais les modifier.

## Installation

Go 1.23 ou plus récent est nécessaire tant que les binaires précompilés ne
sont pas publiés :

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

## Configuration distante

La configuration peut être un fichier public hébergé sur GitHub :

```console
skipack config set https://github.com/owner/repository/blob/main/skipack.yaml
```

Les branches et tags contenant `/` sont résolus via l’API GitHub. Pour une
configuration locale, `skipack` mémorise un chemin absolu dans le répertoire de
configuration utilisateur du système.

## Commandes

| Commande | Description |
| --- | --- |
| `skipack config set <source>` | Enregistre un fichier local ou une URL GitHub publique. |
| `skipack config show` | Affiche la source active. |
| `skipack list` | Liste les groupes et leurs skills. |
| `skipack add <groupe>` | Installe tous les skills du groupe. |
| `skipack update` | Met à jour uniquement les skills suivis dans le projet. |

`add` ignore tout dossier déjà présent sans le modifier. `update` prépare et
valide tous les téléchargements avant de remplacer les versions existantes.

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

## Feuille de route

La V2 prévoit :

- des binaires Windows, Linux et macOS publiés dans GitHub Releases ;
- une matrice de tests et de compilation avec GitHub Actions.

Les dépôts privés, la recherche sur skills.sh, les interfaces graphiques et la
gestion des dépendances entre skills ne font pas partie de la V1.
