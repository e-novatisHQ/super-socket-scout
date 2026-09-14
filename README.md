# Supervision des serveurs du poste

TUI Linux qui inventorie les sockets TCP et UDP en écoute et les relie, lorsque le noyau expose l'information, à leur processus et à leur projet Git.

Elle indique pour chaque écoute : service identifié, niveau de confiance, preuves, gestionnaire, runtime, adresse, port, protocole, exposition hors localhost, PID, commande, dossier, projet, branche, worktree, processus orphelin et processus système.

L'identification suit une hiérarchie explicable : mapping Docker et signatures de commande (`Vite`, `ADB`, `SvelteKit`, etc.), cgroup systemd, puis ports standards. Une déduction par port reste marquée `probable`; un simple nom de processus est `partiel`. Le détail d'un serveur expose les preuves et la raison d'une éventuelle identification incomplète.

## Prérequis et installation

- Linux avec `/proc`, `ss`, `ps` et `git` ;
- Node.js 20.17 ou ultérieur ;
- droits suffisants pour voir et signaler les processus concernés.

```bash
npm ci
npm run tui
```

La vue initiale réduit le bruit : elle affiche d'abord les anomalies, les projets et les applications TCP exposées. Les services système et UDP restent accessibles par les filtres, mais sont repliés par défaut.

Le tableau hiérarchisé est directement navigable : il n'existe pas de seconde liste dupliquée. Les titres de section et les colonnes restent visibles, tandis que chaque ligne de serveur ouvre ses détails avec `Entrée`. Les vues apparaissent comme des onglets selon le cycle `Pertinente → Exposés → Projets → Worktrees → Orphelins → Système → Tous`. `←` et `→` changent d'onglet, et les touches `1` à `7` ouvrent directement une vue. Les filtres restent actifs pendant la session.

La liste ne contient que les serveurs. Les actions globales utilisent des raccourcis permanents : `/` recherche, `u` affiche ou masque UDP, `e` active ou désactive l'identification élevée, `r` rafraîchit et `q` quitte.

L'action **Améliorer l'identification avec sudo** demande l'authentification une seule fois, puis relance les inventaires avec une lecture privilégiée de `ss` et des seuls fichiers nécessaires sous `/proc/<PID>`. Elle aide à attribuer les sockets dont le propriétaire était masqué. Le bandeau indique clairement `Identification élevée`, et l'action inverse revient immédiatement au mode standard. Le programme lui-même n'est jamais relancé en root.

Sélectionner un serveur ouvre son panneau détaillé avec commande, projet, branche, dossier et toutes ses écoutes. Aucun arrêt n'est effectué avant l'affichage du plan et une confirmation finale explicite, réglée sur **Non** par défaut. Le jeton exact `STOP:PID` est réservé au mode CLI automatisé.

## CLI non interactive

```bash
npm run status
node src/cli.mjs status --json
node src/cli.mjs status --sudo
node src/cli.mjs stop --server 'process:1234:987654' --yes --confirm 'STOP:1234'
```

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
