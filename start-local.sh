docker compose -f ./docker-compose.dev.yml down -v --remove-orphans

docker compose -f ./docker-compose.dev.yml build autoscaler

docker compose -f ./docker-compose.dev.yml up
