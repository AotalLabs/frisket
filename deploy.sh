#!/bin/sh

docker build -t aotallabs/frisket:"$TRAVIS_BUILD_NUMBER" .;
docker login -u="$DOCKER_USER" -p="$DOCKER_PASSWORD";
docker push aotallabs/frisket:"$TRAVIS_BUILD_NUMBER";
