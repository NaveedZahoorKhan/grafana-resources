#!/usr/bin/env bash

sudo service apache2 start
docker kill $(docker ps -q)
docker compose up -d
sleep 5

xdg-open http://localhost:3000