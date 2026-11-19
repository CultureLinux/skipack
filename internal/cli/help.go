package cli

import (
	"fmt"
	"io"
)

func (a App) help(w io.Writer) {
	fmt.Fprint(w, `skipack — install and manage packs of agent skills

Usage:
  skipack <command> [arguments]

Commands:
  config set <source>  Set a local YAML file or public GitHub file as config
  config show          Show the active configuration source
  agent set <agent>... Choose codex (.agents), claude (.claude) or both; tracked skills are copied locally
  agent show           Show the agents of the current project
  list                 List configured groups and skills
  audit                Verify configured skills without installing them
  add <group>...|--all Install skills from one or more groups, or all configured skills
  delete <group>...    Delete tracked skills from one or more groups
  delete --all         Delete every skill tracked in the current project
  update [--dry-run]   Refresh the GitHub cache, then update tracked skills (--dry-run only lists changes)
  self-update          Replace skipack with the latest GitHub release
  version              Show the installed skipack version
  help                 Show this help

Examples:
  skipack config set ./skipack.yaml
  skipack config set https://github.com/CultureLinux/skipack/blob/main/skipack.yaml
  skipack agent set codex claude
  skipack list
  skipack add reflex design-pro php docker github-actions gitlab-ci tailwind refactoring-pro planning debugging
  skipack add --all
  skipack delete reflex
  skipack delete --all

Skills are installed in <agent dir>/skills relative to the current directory:
.agents/skills for codex (default), .claude/skills for claude.
`)
}
