package main

import "fmt"

func completionScript(shell string) (string, error) {
	switch shell {
	case "bash":
		return `_sss() {
  local cur prev
  COMPREPLY=()
  cur="${COMP_WORDS[COMP_CWORD]}"
  prev="${COMP_WORDS[COMP_CWORD-1]}"
  if [[ "$prev" == "--port" ]]; then return; fi
  COMPREPLY=( $(compgen -W "status check history run stop completion --help --version --json --sudo --port --server --all --yes --confirm --include-system --name" -- "$cur") )
}
	complete -F _sss sss
`, nil
	case "zsh":
		return `#compdef sss
_arguments \
  '1:command:(status check history run stop completion)' \
  '*::option:(--help --version --json --sudo --port --server --all --yes --confirm --include-system --name)'
`, nil
	case "fish":
		return `complete -c sss -f
complete -c sss -n '__fish_use_subcommand' -a 'status check history run stop completion'
complete -c sss -l help -d 'Afficher l’aide'
complete -c sss -l version -d 'Afficher la version'
complete -c sss -l json -d 'Sortie JSON'
complete -c sss -l sudo -d 'Identification élevée'
complete -c sss -l port -r -d 'Port à vérifier'
complete -c sss -l server -r -d 'Identifiant du serveur'
complete -c sss -l name -r -d 'Nom de l’association'
`, nil
	default:
		return "", fmt.Errorf("shell non pris en charge : %s", shell)
	}
}
