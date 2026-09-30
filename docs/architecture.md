# Architecture de l'exécutable Go

L'exécutable conserve trois frontières explicites :

1. **Interface** — rendu TUI, contrat clavier et CLI. `renderScreen` et `detailScreen` sont des fonctions pures recevant données, dimensions et horloge ; elles ne découvrent ni ne modifient aucun processus.
2. **Orchestrateur** — `serverOrchestrator` impose le passage découverte → plan → confirmation → exécution. Il protège les processus système dans le parcours interactif et rend l'adaptateur injectable en test.
3. **Adaptateur métier** — `linuxServerAdapter` collecte `/proc`, `ss`, Git et Docker. `BuildPlan` ne mute rien ; `Execute` revalide PID et heure de démarrage avant tout signal.

Les capacités transverses sont isolées par responsabilité :

- `developer_actions.go` : copie et ouverture explicites, sans shell ;
- `associations.go` : wrapper de processus et registre sans arguments de commande ;
- `preferences.go` : configuration atomique privée ;
- `history.go` : historique expurgé et borné ;
- `port_check.go` : préflight et contrat JSON versionné ;
- `completion.go` : intégration Bash, Zsh et Fish ;
- `redaction.go` : masquage des valeurs sensibles avant exposition.

L'implémentation Node reste la référence de parité pour la découverte, la classification, les plans d'arrêt, les codes de sortie et leur forme JSON historique. Les fonctions Go ajoutées disposent de contrats versionnés séparément afin de ne pas casser cette compatibilité.
