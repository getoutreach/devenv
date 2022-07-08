# Development @ Outreach

We build a few kinds of applications today at Outreach. They tend to have different needs in terms of dev machine setup and actual code runtime. Instead of trying to provide once completely unified dev experience, we try to tailor the tooling for the task, and developer expectations.

This document is a high level overview of how we use `devenv` to build different kinds of applications.

## Bootstrap services

Bootstrap microservices (overwhelming number of repos) run in Kubernetes in production and get their configuration from config maps and secrets. Instead of having local machine friendly way of configuring these, doing service discovery (for dependencies), we support running in Kubernetes during development as well.

### Running in Kubernetes

`devenv` provides two deployment modes

1. released version of the service via the latest pushed docker image into our image repository
2. from source lets developers make changes, deploy them to `devenv` cluster, run and debug.

#### Pre-built Service

In case of pre-built services manifests from git main branch are used in combination with image from CI.

> Note these two can probably be different versions in rare cases when the image hasn't been built yet, but the manifests were merged.

#### Running From Source Code

`devenv apps deploy` 

* builds `jsonnet` manifests
* builds an image from source in the `devenv` cluster
* uses the new built image in kubernetes manifests
* applies the manifests to `devenv` cluster

`devenv apps dev`

* builds `jsonnet` manifests
* applies the manifests to `devenv` cluster
* scales down deployment
* creates new pod in the devenv cluster, with the same labels, mounts, environment variables as the pods in deployment
* syncs source code from local machine to the new replacement pod
* opens terminal session into this replacement pod
* port forwards the service (on port 5000 for gRPC, on port 8080 for HTTP) and metrics server (on port 8000)

> source code keeps in sync with local machine

The dev pod has the tools and credentials necessary to install packages and run the service.

The same environment variables and mounts are available as well in order for the service to have the right configuration for running.

Developers can build and start the service, run tests, including e2e.

#### Performance

The development pods are not going to stay around forever and need to be recreated when manifests and Dockerfile change. With cloud devenv they also need to be recreated after the cluster wakes up from sleep.

This leads to long time to ready/run in such a container. To avoid this, nightly build creates images with latest packages built in, Buildkit and dev pod share package cache, so devs don't have to redownload them all the time.

## Client

OrCA/Flagship client has similar performance needs as Giraffe, but without the need of running in Kubernetes (no need for local configuration or service discovery). In production it comes from a CDN. Local development server is used in combination with a reverse proxy. This reverse proxy serves requests to services from the development cluster, and request for client side resources from local server.

### Development Server (today)

OrCA today runs webpack dev server locally, in two modes

1. Standalone, doesn't require devenv on local box, runs only OrCA
2. Devenv, requires devenv, uses localizer to proxy requests through devenv cluster, still runs webpack dev server locally

### Reverse Proxy

The localizer proxy pod is replaced with reverse proxy built into devenv. In this mode, webpack dev server doesn't need to have certificates, `devenv` would take care of it.

The reverse proxy can be started as another process when starting webpack dev server for Standalone development.

> alternative would be to start webpack dev server through `devenv apps dev`. this would make it easier to update `/etc/hosts` file if necessary.

With a proxy running locally, there doesn't need to be a direct connection to cluster as the traffic from https://bento1a.outreach-dev.com/ is directeted by the reverse proxy.

## Flagship Server & Worker

Flagship runs in Kubernetes as well, but being a legacy codebase, built on Ruby on Rails, having more than one "mode" (server, worker, client), being rather large and having long history (meaning .git folder is BIG) presents special needs for runtime. We sync the local code into a container running in kubernetes with gems already built in.

## Giraffe

Giraffe as a Node.js service needs as much performance from a dev machine as we can get. It takes a while to build. It's hard and expensive to match modern ARM Mac performance at building Javascript, and so the best tool available is used - developer's machine. Giraffe is built locally, and then synced to a container in Kubernetes. 
