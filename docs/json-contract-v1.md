# Contrat JSON v1

Les sorties JSON de `server-watch` suivent le contrat public v1. Les champs documentés ici ne changent pas de nom ou de type dans une version compatible. De nouveaux champs optionnels peuvent être ajoutés ; les consommateurs doivent ignorer ceux qu'ils ne connaissent pas.

## `status --json`

Retourne un tableau de serveurs. Chaque serveur possède au minimum :

- `id`, `label`, `protocol`, `address`, `port` ;
- `serviceName`, `manager`, `confidence` ;
- `exposed`, `system`, `worktree`, `orphan`, `elevated` ;
- `endpoints` et `evidence`, toujours sous forme de tableaux.

Les métadonnées indisponibles sont omises lorsqu'elles portent `omitempty`. `id` combine le PID et son heure de démarrage lorsque le processus est visible. Un identifiant `unknown:<protocol>:<port>` n'autorise jamais un arrêt.

## `check --json`

Retourne `{ "schemaVersion": 1, "checks": [...] }`. Chaque contrôle contient `port`, `state` et `owners`. `state` vaut `free`, `occupied` ou `unavailable`. `owners` est toujours un tableau et ne contient jamais de ligne de commande.

## `history --json`

Retourne `{ "version": 1, "records": [...] }`. Un enregistrement contient sa clé, son identité réseau, ses dates `firstSeen` et `lastSeen`, ainsi que `active`. L'historique ne conserve pas les commandes observées.

## `stop --json`

Retourne `code`, `plan`, `results` et éventuellement `error`. Le plan expose `actions`, `conflicts` et `confirmationToken`. `results` et `conflicts` restent des tableaux, y compris lorsqu'ils sont vides.

Codes communs : `0` succès, `1` erreur technique ou détection indisponible, `2` conflit, `3` entrée invalide, `4` confirmation requise, `130` annulation.
