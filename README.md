# Supervision des serveurs du poste

## Prototype exécutable Go

L’implémentation Go sans dépendance tierce produit un binaire Linux statique :

```bash
make build
./bin/server-watch status
./bin/server-watch
```

Le binaire Go couvre la même interface que la version Node de référence : inventaire et identification, vues responsives, recherche, détails, lecture élevée avec sudo et arrêt gracieux confirmé.

TUI Linux qui inventorie les sockets TCP et UDP en écoute et les relie, lorsque le noyau expose l'information, à leur processus et à leur projet Git.

Elle indique pour chaque écoute : service identifié, niveau de confiance, preuves, gestionnaire, runtime, adresse, port, protocole, exposition hors localhost, PID, commande, dossier, projet, branche, worktree, processus orphelin et processus système.

L'identification suit une hiérarchie explicable : mapping Docker et signatures de commande (`Vite`, `ADB`, `SvelteKit`, etc.), cgroup systemd, puis ports standards. Une déduction par port reste marquée `probable`; un simple nom de processus est `partiel`. Le détail d'un serveur expose les preuves et la raison d'une éventuelle identification incomplète.

## Prérequis et installation

- Linux avec `/proc`, `ss`, `ps` et `git` ;
- aucun runtime Go ou Node pour utiliser un binaire précompilé ;
- Go 1.24 ou ultérieur uniquement pour compiler le binaire autonome ;
- Node.js 20.17 ou ultérieur uniquement pour exécuter la version de référence et ses tests ;
- droits suffisants pour voir et signaler les processus concernés.

```bash
npm ci
npm run tui
```

Ou, sans runtime Node après compilation :

```bash
make build
./bin/server-watch
```

Installation dans `~/.local/bin` et vérification :

```bash
make install
server-watch --version
```

Les complétions sont générées sans écrire dans le profil du shell :

```bash
server-watch completion bash
server-watch completion zsh
server-watch completion fish
```

`make release VERSION=x.y.z` construit les binaires Linux `amd64` et `arm64` dans `dist/` avec un fichier `SHA256SUMS`. La version est injectée à la compilation. Le [contrat JSON v1](docs/json-contract-v1.md) documente les champs et les règles de compatibilité destinés aux scripts et intégrations.

L'[architecture Go](docs/architecture.md) sépare l'interface pure, l'orchestrateur de sûreté et l'adaptateur Linux injectable.

La vue initiale **Attention** réduit le bruit : elle ne montre que les éléments qui demandent une action ou une vérification. Un résumé décisionnel distingue immédiatement `Action recommandée`, `À vérifier` et `Sans anomalie`.

Le tableau hiérarchisé est directement navigable : il n'existe pas de seconde liste dupliquée. Les sections portent le diagnostic une seule fois et les lignes répondent dans l'ordre à trois questions : quel service, dans quel contexte et sur quelle écoute ? Les détails comme le PID, la commande, la branche, le runtime et les preuves restent accessibles avec `Entrée`. Les quatre onglets `Attention → Projets → Système → Tous` sont intégrés dans un séparateur visuel, sans libellé redondant. `←` et `→` changent d'onglet, et les touches `1` à `4` ouvrent directement une vue. Le nombre affiché est rapporté au total de la vue afin de rendre l'effet des filtres explicite.

Une phrase sous la ligne sélectionnée explique le diagnostic et indique l'action utile, par exemple utiliser `e` lorsque le propriétaire d'une écoute externe est masqué.

Les processus orphelins sont regroupés sous `ORPHELINS — ACTION REQUISE`. Leur contexte indique immédiatement `Worktree disparu` ou `Dossier disparu`; le détail conserve la cause et le dernier chemin connu.

Le tableau s'adapte à la largeur du terminal. Sous 80 colonnes, il conserve seulement le service et l'écoute. Entre 80 et 119 colonnes, le contexte reçoit tout l'espace restant. À partir de 120 colonnes, une colonne runtime apparaît. La largeur utile est plafonnée à 130 colonnes afin de préserver une lecture compacte.

Les libellés très répétés sont abrégés dans l'inventaire (`App`, `Non identifié`, `ext`, `loc`, `ident.`). Le panneau détaillé conserve les appellations et adresses complètes.

La liste ne contient que les serveurs. Les actions globales utilisent des raccourcis permanents : `/` recherche, `u` affiche ou masque UDP, `t` alterne les tris par priorité, port et projet, `e` active ou désactive l'identification élevée, `w` active le suivi automatique toutes les deux secondes, `r` rafraîchit, `?` affiche l'aide et `q` quitte. L'inventaire n'est plus recalculé pendant une simple navigation et la sélection est restaurée par l'identifiant stable du serveur après chaque collecte.

La hauteur du terminal est également prise en compte. Lorsque toutes les lignes ne tiennent pas, la liste défile autour de la sélection, affiche sa position dans l'inventaire et conserve le diagnostic ainsi que les raccourcis essentiels à l'écran.

Les actions contextuelles accélèrent les gestes quotidiens : `c` copie l'URL HTTP reconnue, puis à défaut le dossier ou le PID ; `o` ouvre uniquement une URL HTTP déduite d'un service ou port connu ; `p` révèle le dossier de travail ou le projet lorsqu'il existe encore. Elles ne transmettent jamais la commande observée à un shell. La vue, l'affichage UDP, le tri et le suivi sont mémorisés dans la configuration utilisateur ; l'élévation sudo reste volontairement limitée à la session courante.

L'action **Améliorer l'identification avec sudo** demande l'authentification une seule fois, puis relance les inventaires avec une lecture privilégiée de `ss` et des seuls fichiers nécessaires sous `/proc/<PID>`. Elle aide à attribuer les sockets dont le propriétaire était masqué. Le bandeau indique clairement `Identification élevée`, et l'action inverse revient immédiatement au mode standard. Le programme lui-même n'est jamais relancé en root.

Sélectionner un serveur ouvre son panneau détaillé avec commande, projet, branche, dossier et toutes ses écoutes. Les valeurs d'options et variables reconnues comme mot de passe, jeton, secret ou clé sont masquées avant toute sortie. Le détail s'enveloppe et défile avec `↑/↓` lorsque la hauteur est limitée, tandis que sa position et ses actions restent visibles. Aucun arrêt n'est effectué avant l'affichage du plan et une confirmation finale explicite, réglée sur **Non** par défaut. Les processus système restent protégés dans la TUI ; le jeton exact `STOP:PID` est réservé au mode CLI automatisé.

## CLI non interactive

```bash
npm run status
node src/cli.mjs status --json
node src/cli.mjs status --sudo
node src/cli.mjs stop --server 'process:1234:987654' --yes --confirm 'STOP:1234'

# mêmes commandes avec le binaire autonome
./bin/server-watch status
./bin/server-watch status --json
./bin/server-watch status --sudo
./bin/server-watch check --port 5173 --port 8000 --json
./bin/server-watch history --json
./bin/server-watch run --name storefront -- npm run dev
./bin/server-watch stop --server 'process:1234:987654' --yes --confirm 'STOP:1234'
```

`check` sert de préflight avant un lancement et distingue `free`, `occupied` et `unavailable`; un port occupé retourne le code `2`. `history` restitue les apparitions et disparitions observées sans enregistrer les lignes de commande. `run` lance directement la commande fournie, sans shell, et associe ses descendants réseau au nom et au dossier choisis pendant toute leur durée de vie. Le registre d'association ne conserve pas les arguments de la commande.

Pour valider les deux implémentations : `make test`. Pour un contrôle manuel sans risque, lancer `./bin/server-watch status`, puis la TUI avec `./bin/server-watch`; `q` quitte et aucun arrêt n'est possible sans ouvrir un détail, choisir `s`, puis confirmer explicitement.

Pour préparer un arrêt sans l'exécuter, omettre `--yes` ou fournir un jeton incorrect : le plan et le jeton attendu seront affichés et le code de sortie sera `4`.

`status --sudo` active explicitement la même collecte privilégiée en lecture seule. `--all` vise tous les processus utilisateur identifiables. Les processus système sont exclus et protégés par défaut. `--include-system` lève seulement cette protection logique : les permissions Linux continuent de s'appliquer, et l'élévation d'inventaire n'est jamais réutilisée pour envoyer un signal.

## Modèle de sûreté

- L'inventaire et la construction du plan sont en lecture seule.
- L'identité combine socket et PID ; l'heure de démarrage du processus est revalidée avant le signal.
- L'arrêt envoie `SIGTERM` au lanceur et à ses descendants lorsque leur chaîne locale est identifiable.
- Aucun `SIGKILL`, téléchargement automatique ou arrêt d'un PID non identifiable. `sudo` est optionnel et limité à l'inventaire en lecture seule.
- Un socket sans PID reste visible mais ne peut pas être sélectionné.
- Un worktree dont le dossier a disparu est marqué `ORPHELIN`.

Codes de sortie : `0` succès, `1` erreur technique, `2` conflit, `3` entrée invalide, `4` confirmation requise, `130` annulation.

## Limites

La correspondance projet dépend du dossier courant du processus et de Git. Un service de conteneur apparaît généralement comme son proxy hôte ; Portainer/Docker peut donc nécessiter une inspection séparée. Selon la politique `sudo` du poste, certains processus peuvent rester masqués même en identification élevée.
