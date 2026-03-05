# Ferry

Self-host anything without hassle. Inspired by Fly, Coolify and Kamal.

Ferry is a lightweight terminal based alternative to Coolify. No runtime overhead. Config as code. Just a single CLI to deploy, preview and manage your Docker containers on any VPS.

Ferry is conceptually similar to Kamal, but does not and gives more out of the box. Preview deployments, secure your VPS and scale resources all from a CLI.  

Ferry allows you to deploy Docker based apps to your own VPS. It provides a simple way to manage deployments, environment variables, logs, databases, rollbacks and more across multiple servers. And the beauty of it costs you nothing on top of your existing VPS hosting. It's stateless, config-as-code and no overhead on your machine. 

Ferry is build on proven, boring technologies that do not promote any vendor lock-in. It's built on top of Traefik, Docker, Go and SSH. 

- 💻 Deploy any application from a Dockerfile or docker-compose file
- ✊ Zero downtime deployment
- 🌏 High availability and load balancing
- 🔒 Zero config SSL Certs and auto renewal
- 🔑 Deploy and update environment variables with SOPS, Ansible Vault or Infisical
- 📄 View logs in real time
- 🔄 Rollback to previous versions
- 🚀 One command deployment
- Preview changes before deployment on a custom domain 
- Setup your VPS with a single command

<div align="center">
  <img src="./assets/gopher.png" alt="Golang gopher on a Ferry" width="300">
  <br>
  <img src="https://img.shields.io/github/license/ronxjansen/ferry" alt="GitHub">
  <img src="https://img.shields.io/github/go-mod/go-version/ronxjansen/ferry" alt="GitHub go.mod Go version">
  <img src="https://img.shields.io/github/v/tag/ronxjansen/ferry" alt="GitHub tag (latest SemVer)">
</div>

## Commands

- init
- exec - run a remote command through SSH (used in other commands)
- proxy - abstract away all Traefik related logic (or kamal-proxy logic); used in preview and deploy
- logs - basical
- deploy - abstracts away all docker related deployment logic
- rollback
- env - abstracts away all ENV related logic (or infisical, sops, ansible vault)
- preview

## Installation

```
go install github.com/ronxjansen/ferry@latest
cd ferry
go install
```

## Getting started

Before you can get started, this is what you will need: 

- a Ubuntu LTS VPS with SSH access to it
- Traefik, SOPS and Docker installed on your VPS 
- a git repo with a Dockerfile

That's it!

## Prerequisites

- Go 1.16 or later 
- SSH access to remote servers
- a Ubuntu LTS VPS with SSH access to it

## Contributing

Contributions are welcome! Please open an issue or submit a PR.

## Motivation

[Kamal](https://github.com/basecamp/kamal) and [Sidekick](https://github.com/MightyMoud/sidekick) are two major inspirations for writing this tool. Kamal is a great solution, but Ruby based. Sidekick is the Golang based Kamal alternative, yet very early stage. I've considered to write a few PR's, but considering the scope of the changes I wanted to bring, I decided to built it from scratch. 

Ferry is intended to be less opinionated (you can use it without sops), has support for databases, allows you to inspect your production containers, monitor CPU, memory and log traffic in real time.  

## Roadmap

v0.1
- [ ] Deploy databases 
- [ ] Support migrations

v0.1.1
- [ ] Support all docker compose features, depends_on, env vars, health checks, volumes, networks, labels, etc as individual features on exec
- [ ] Use docker compose as source of truth and ferry config as overrides

v0.2
- [ ] Make CLI output cleaner. Add --verobse and --json flags to deploy command
- [ ] Generate a preview of the changes before applying them
- [ ] Add status command that shows config, current state and diff. Also some stats from docker stats
- [ ] Parse current Config and parse current docker containers running and build config state from that
- [ ] Add SOPS, or Infisical or Ansible Vault support (maye can we manage this through a CLI / interactive editor?)
- [ ] Support more docker registry types (gcr, gcp, ecr, etc.)

v0.3
- [ ] Asset bridging
- [ ] Add `vps doctor` command to check if your VPS is ready to go (has docker, ssh access)
- [ ] Support multiple server nodes 
- [ ] Add `init` command to create a configuration - a tui based wizard to generate a config yaml
- [ ] Local build and deploy
- [ ] Hooks - run scripts before, during and after deploy
- [ ] Rolling restarts
- [ ] Rollback
- [ ] Better zero downtime deployment (we now only rely on Traefik)
- [ ] Add support for more Linux distros
- [ ] Minimal dashboard + metrics in a terminal UI (traffic, CPU, memory, etc.)
- [ ] Use Kamal-proxy instead of Traefik? Make the proxy optionalable. Users can deploy with Traefik proxy as well.  

## Out of scope

At least for the forseeable future, the following features are not in scope:

- Load balancing - you can build and deplly your own load balancer, using Ferry
- VPS provisioning (setting up Traefik, Docker, SOPS, etc.)
- VPS SSH and firewall hardening
- Windows support
