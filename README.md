<!-- Formatted by https://github.com/quilicicf/markdown-formatter -->

# docker-autoscaler

## Problemes de l'ancien autoscaler

- Sécurité
  - Besoin d'exposer un endpoint public pour que github puisse envoyer les webhooks
- Praticité
  - Besoin d'un reverse proxy pour exposer l'endpoint
  - Compliqué de dev dessus, il faut mettre en place un autre webhook sur github via une api externe (smee.io dans mon cas) pour pouvoir recevoir les events de github en local
  - La mise à jour du runner est compliquée, il faut d'abord faire un commit sur le repo docker-action-runner, puis livrer cette image, puis récupérer le tag de l'image livrée, puis mettre à jour le tag utilisé par l'autoscaler avant de redéployer l'autoscaler
- Bugs/Problemes
  - Le token d'authentification d'un runner est stocké dans un volume docker, sauf que parfois le token est invalidé par github, donc le prochain runner qui démarre ne peut pas s'authentifier avec le token sauvegardé, et donc ne démarre pas
  - Les jobs en attente de runner vont prendre le prochain runner à démarrer, ce qui peut mener à des interrogations d'un utilisateur sur pourquoi le job ne se lance pas (c'est un autre job qui était en attente qui a été lancé sur le runner qui a démarré par le job de l'utilisateur)
  - Un job dont le conteneur a crashé pour une raison ou pour une autre ne sera pas relancé automatiquement (il n'y aura pas de webhook qui sera renvoyé)
  - Le multi hote est supporté, mais ne permet pas de faire du roundrobin sur les hotes
  - L'image du runner est lourde et compliquée à comprendre, il y a plusieurs scripts bash qui permettent : d'installer la runtime des actions, de générer un token, d'installer les dépendances operis, de lancer le conteneur correctement. Ces scripts rajoutent beaucoup de complexité

## Réglés sur la nouvelle solution

- Sécurité
  - L'autoscaler se connecte directement à github sur <https://broker.actions.githubusercontent.com/scalesets/message> et récupere les messages présents dans la queue
- Praticité
  - Plus besoin de reverse proxy, l'autoscaler se connecte directement à github
  - Plus besoin de webhook externe pour dev en local, l'autoscaler se connecte directement à github, donc beaucoup plus simple pour dev en local, il y a également un fichier docker compose qu'il possible de lancer (en remplissant correctement les variables d'environnement) pour lancer l'autoscaler en local
  - La mise à jour du runner est plus simple car l'image du runner est présente dans le repo de l'autoscaler, et elle est également plus simple à comprendre (FROM ghcr.io/actions/actions-runner:VERSION)
- Bugs Résolus
  - Un token d'authentification unique est généré pour chaque conteneur lancé par la librairie scaleset, puis est passé au runner via une variable d'environnement
  - Les jobs en attente ne sont pas pris par le prochain runner à démarrer, il y a un mécanisme de retry qui va renvoyer un message dans la queue au bout de quelques minutes si le job n'a pas été pris par un runner, ce qui permet de relancer le job sur un autre runner
  - Le point au dessus regle le problème des jobs dont le conteneur a crashé, car le message sera renvoyé dans la queue et pourra être pris par un autre runner
  - Multi hote supporté, et j'ia mis en place un mécanisme de roundrobin sur les hotes, donc les jobs sont répartis sur les hotes de manière équitable
  - L'image du runner est simple et consiste en un FROM ghcr.io/actions/actions-runner:VERSION, puis l'installation des dépendances operis via un script bash, puis un autre script bash sert d'entrypoint pour démarrer et configurer docker correctement pour pouvoir mettre du cache docker en place

## Basé sur le projet scaleset de github : <https://github.com/actions/scaleset>

## TODO

- [x] Bugs qui crashent l'appli
  - [x] Scaleset déja existant => crash
  - [x] Création d'un conteneur impossible => crash
  - [x] Arrêt d'un conteneur impossible => crash
- [x] Récupérer l'image du runner au démarrage => l'image du runner étant présente dans le repo, il n'y aura pas de mise à jour de l'image sans mise à jour de l'autoscaler
- [x] Mettre en place un moyen de définir plusieurs hotes pour le runner (pour pouvoir scaler sur plusieurs hôtes)
- [x] Mettre en place un cache docker interrunner => impossible via le partage de /var/lib/docker, docn il faut  :
  - [x] Mettre en place un registry miroir pour cacher les pulls des images docker dans les runners => OK
  - [x] Mettre en place un volume docker pour que les utilisateurs puissent partager le cache via docker buildx build --cache-from --cache-to => OK
- [x] Mettre en place un cache pour asdf/maven => maven OK, asdf bizarre
- [x] Supprimer cobra de l'autoscaler et utiliser un simple main.go
  - [x] Lister toutes les variables d'environnement et les passer en paramètre à l'autoscaler
  - [ ] Lister les variables d'environnement et expliquer à quoi elles servent dans le README
- [x] Mettre en place le fichier docker compose de dev => OK
  - [x] Autoscaler
  - [x] Un registry miroir pour cacher les pulls des images docker dans les runners
  - [x] Pas besoin de reverse proxy car l'autoscaler se connecte directement à github
- [ ] Mettre en place les workflows github
  - [ ] 1 Workflow (test) pour build l'autoscaler et lancer les tests
  - [ ] 1 Workflow (livraison) pour build et push les images docker de l'autoscaler et du runner
  - [ ] 1 Workflow (déploiement) pour déployer l'autoscaler sur une machine
  - [ ] 1 Workflow (test-local) pour pouvoir tester l'autoscaler en local (similaire à test autoscaler sur le repo gh runner autoscaler)
- [ ] Créer un nouveau resource group autoscaler avec un serveur principal sur lequel tourne l'autoscaler, et unou plusieurs runners sur lesquels l'autoscaler va scaler les runners
- [ ] Avoir des logs par runner, stockés dans des fichiers
- [ ] Fixer les pb de sécurité latest etc
- [ ] Refaire le fichier e2e\_test.go pour tester l'autoscaler sur la ci
