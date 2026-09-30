package main

import "fmt"

func completionScript(shell string) (string, error) {
	switch shell {
	case "bash":
		return `_server_watch() {
  local cur prev
  COMPREPLY=()
  cur="${COMP_WORDS[COMP_CWORD]}"
  prev="${COMP_WORDS[COMP_CWORD-1]}"
  if [[ "$prev" == "--port" ]]; then return; fi
  COMPREPLY=( $(compgen -W "status check history run stop completion --help --version --json --sudo --port --server --all --yes --confirm --include-system --name" -- "$cur") )
}
complete -F _server_watch server-watch
`, nil
	case "zsh":
		return `#compdef server-watch
_arguments \
  '1:command:(status check history run stop completion)' \
  '*::option:(--help --version --json --sudo --port --server --all --yes --confirm --include-system --name)'
`, nil
	case "fish":
		return `complete -c server-watch -f
complete -c server-watch -n '__fish_use_subcommand' -a 'status check history run stop completion'
complete -c server-watch -l help -d 'Afficher l’aide'
complete -c server-watch -l version -d 'Afficher la version'
complete -c server-watch -l json -d 'Sortie JSON'
complete -c server-watch -l sudo -d 'Identification élevée'
complete -c server-watch -l port -r -d 'Port à vérifier'
complete -c server-watch -l server -r -d 'Identifiant du serveur'
complete -c server-watch -l name -r -d 'Nom de l’association'
`, nil
	default:
		return "", fmt.Errorf("shell non pris en charge : %s", shell)
	}
}
