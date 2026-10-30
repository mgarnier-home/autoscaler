# autoscaler

Démarre des runners GitHub Actions à la demande, dans des conteneurs Docker, à partir de la
bibliothèque `github.com/actions/scaleset`. L'autoscaler se connecte lui-même à GitHub : pas de
webhook ni de reverse proxy. Il est déployé par la stack `autoscaler` du repo `orchestrator`, et
fournit les runners `linux/amd64` et `linux/arm64` utilisés par les workflows des autres repos.

Le contexte et l'historique des choix sont dans `README.md`.

## Commandes

| Action                               | Commande            |
| ------------------------------------ | ------------------- |
| Télécharger les dépendances          | `task download`     |
| Compiler (`autoscaler/dist/`)        | `task build`        |
| Construire l'image du runner         | `task build-runner` |
| Compiler et lancer en local          | `task start`        |
| Tests                                | `go test ./...` dans `autoscaler/` |

`task start` enregistre un vrai scale set auprès de GitHub et lance des conteneurs : demande
avant de le lancer. Le module Go est dans `autoscaler/`, pas à la racine.

## Arborescence

```
autoscaler/main.go     point d'entrée
autoscaler/api/        API HTTP : GET /stats (lu par le dashboard homepage) et GET /version
autoscaler/config/     configuration lue depuis l'environnement
autoscaler/github/     client GitHub et scale set
autoscaler/scaler/     décision de démarrage et d'arrêt des runners, état des runners
autoscaler/docker/     création et arrêt des conteneurs sur les hôtes Docker
autoscaler/utils/      journalisation
autoscaler/e2e_test.go test de bout en bout
runner/                image du runner (Dockerfile, scripts/install.sh, scripts/entrypoint.sh)
docker-compose.yml     environnement de dev : buildkit, autoscaler, runner
```

## Configuration

Variables lues par `autoscaler/config/config.go` : `REGISTRATION_URL`, `GITHUB_TOKEN`,
`SCALE_SET_NAME`, `RUNNER_GROUP`, `LABELS`, `MIN_RUNNERS`, `MAX_RUNNERS`, `RUNNER_IMAGE`,
`RUNTIME`, `DOCKER_HOSTS`, `DOCKER_REGISTRY_URL`, `DOCKER_REGISTRY_USERNAME`,
`DOCKER_REGISTRY_PASSWORD`, `BUILDKIT_HOST_URL`, `PIPE_RUNNER_LOGS`,
`DELETE_SCALE_SET_ON_SHUTDOWN`, `LOG_LEVEL`, `LOG_FORMAT`, `HTTP_PORT` (port de l'API, 8080 par
défaut), `DOCKER_NETWORK`.

`DOCKER_NETWORK` rattache les runners à un réseau docker, pour qu'ils joignent buildkit par son nom
(`BUILDKIT_HOST_URL: tcp://buildkit:1234`). Ce réseau doit être défini par l'utilisateur, avec un
`name:` fixe s'il est créé par compose, et exister sur chaque hôte de `DOCKER_HOSTS` :
l'autoscaler refuse de démarrer sinon. Vide, les runners restent sur le réseau par défaut.

## Publication

- `.github/workflows/test.yml` : `go vet` et `go test -race` sur chaque pull request vers `main` et
  chaque push sur `main`.
- `.github/workflows/publish.yml`, lancé par le push d'un tag `vX.Y.Z` présent sur `main` : publie
  `mgarnier11/autoscaler` et `mgarnier11/runner` (amd64 et arm64), étiquetées `amd64`, `arm64`,
  avec la version et `latest`. La version, le commit et la date sont injectés dans `buildinfo` de
  `utils` et servis par `GET /version`.

## Définition de « terminé »

Dans `autoscaler/` : `go build ./...`, `go vet ./...` et `go test ./...` passent. Toute nouvelle
logique arrive avec ses tests.

Le test de bout en bout (`e2e_test.go`) est ignoré sauf avec `E2E=true` ; il enregistre un vrai
scale set auprès de GitHub, ne l'active pas sans accord.

## Interdits

- Ne pas lire `.env`.
- Ne pas lancer le workflow de publication, ne pas pousser de tag ni d'image Docker.
