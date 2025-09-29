<div align="center" style="padding-bottom: 8px;">
  <img alt="ghat" height="280px" src="./docs/assets/ghat-final-logo-with-background-shadow.png">
</div>

# GHAT(D)

GHAT(D) is an open-source, opinionated, and free full-stack web application framework based on the Go programming language. Its name is an acronym that stands for Go, HTMX, Alpine.js, Tailwind, and DaisyUI, which originally formed the foundational stack. Over time, for improved usability, it has also been extended to support most Vite-compatible frameworks (tested with Vue). The aim is to make GHAT(D) a solid foundation for creating highly portable, scalable, and performant full-stack projects. Whether you need just a backend, a landing page, or even a blog (coming soon), you can still utilise the GHAT(D) framework.

We recognise that everyone has unique needs, and ideally their solutions should not start with a messy foundation that requires cleaning up before building. To reduce cognitive load and make preparation easier, we have introduced "builder blocks" which we call `Details`. A `Detail` is an independent application that can function both within the GHAT(D) framework and on their own. At present, we only support `api` and `web` typed `Details`.

## Motivation

GHAT(D) is a hobby project I work on in my spare time. This project is designed to provide a good "getting started" framework for people like me who are interested in Go, APIs, and Web Applications and want a consistent base & standards on which to build projects. I hope that this framework can be used as a foundation for building out many awesome projects and initiatives.

I also aim to use this project as a learning opportunity, to improve my understanding of and share my knowledge of lightweight frontend libraries, highly portable full-stack alternatives, and cost-effective infrastructure solutions for full-scale products. 

As we develop this project, I want to also create tutorials/guides for those who want to integrate it with other technologies, such as `rpc`,  `graphql`,  `websocket`, and others.

I am a platform engineer by trade, so I do not promise perfect code by any stretch of the imagination (especially with the front end - so please support and contribute). Instead, I am aspiring to create a standardised framework that helps those curious (about Go, APIs, hubby projects, and web app development) to turn their ideas/ hobbies into tangible product(s) that they can demo and even ship.

This will be an exciting experience, and I look forward to building out this project with you all and sharing my progress and knowledge as it matures.

## Starting locally

Before getting started please make sure you have the correct version of [Go installed](https://go.dev/doc/install) or you can use [ASDF](https://github.com/asdf-vm/asdf) to install it with the following command

```sh
# Add the plugin for Go
asdf plugin-add golang

# Install required version
asdf install
```

### Using the CLI (WIP)

To start using the CLI you can use the code:

```sh
go run cli/cli.go
```

You can then pass your desired cli command with:

```sh
go run cli/cli.go <desired-command>
```

#### TODO: Implementing the `new` command

We are currently implementing the `new` command, which will create a base folder for a new ghat(d) compatible file.

`Example local command`

```shell
go run cli/cli.go new -n "awesome-service" -w "github.com/ooaklee/ghatd-detail-web-demo-landing-dash-and-more,github.com/ooaklee/ghatd-detail-api-demo-endpoints"
```

`Prerequisites`
- [ ] Detail repo for demo-endpoints
- [ ] Detail repo for demo-dash

`Success Criteria`
- The implementation will be considered successful once the user can run the command below and get an output directory with a working app (after running `go mod tidy`)
  - Once tidied, the user should be able to run `go run cmd/server.go start-server` in the output directory and access http://localhost:4000/.  
 

### Starting the server

To start the server you can use the code:

```sh
go run main.go start-server
```

However, for a better development expierence, please install the package [`reflex`](https://github.com/cespare/reflex) which will enable you to hot-reload by rerun a specified command on file change, and running the command:

```sh
reflex -r '\.(html|go|css|png|svg|ico|js|woff2|woff|ttf|eot)$' -s -- go run main.go start-server
```

> More [information on hot-reloading can be found below](#hot-reloading)


## Good to know

### ASCI Art

All ASCI related code in this template was created using [PatorJK](https://patorjk.com/software/taag/#p=display&h=2&f=Isometric3)

### Curl Examples

- Making `GET` resquest: `curl -i -X GET "http://localhost:4000/v0/health/check"`

### How to stop file server showing directory listing?

Add a blank index.html file to the specific directory that you want to disable listings for. For example, the
code below will create an index file which will stop [the web app](http://localhost:4000/static/) from showing 
and listing page.

```sh
touch internal/web/ui/static/index.html
```

### Hot reloading

Install reflex

`go install github.com/cespare/reflex@latest`

> You can find more information in the repo https://github.com/cespare/reflex

Once installed, run the server

```sh
reflex -r '\.(html|go|css|png|svg|ico|js|woff2|woff|ttf|eot)$' -s -- go run main.go start-server
```

### How to build binaries

One of the benefits of using the GHATD stack is that it compiles everything into a single binary. This makes it highly portable and provides numerous deployment options. 

#### CLI 

To build a binary for the GHATDCLI for your desired system architecture, please follow the instructions below:


> All commands should be executed from the root directory.

##### Mac OS (ARM64)

```sh
export BINARY_NAME=ghatdcli
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -a -installsuffix cgo -ldflags="-w -s" -o ./$BINARY_NAME cli/cli.go
```

##### Mac OS (AMD64)

```sh
export BINARY_NAME=ghatdcli
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -a -installsuffix cgo -ldflags="-w -s" -o ./$BINARY_NAME cli/cli.go
```

##### Linux (ARM64)

```sh
export BINARY_NAME=ghatdcli
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -a -installsuffix cgo -ldflags="-w -s" -o ./$BINARY_NAME cli/cli.go
```

##### Linux (AMD64)

```sh
export BINARY_NAME=ghatdcli
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -ldflags="-w -s" -o ./$BINARY_NAME cli/cli.go
```


#### Web App

To build a binary for web app to your desired system architecture, please follow the instructions below:

> All commands should be executed from the root directory.

##### Mac OS (ARM64)

```sh
export BINARY_NAME=ghatd
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -a -installsuffix cgo -ldflags="-w -s" -o ./$BINARY_NAME main.go
```

##### Mac OS (AMD64)

```sh
export BINARY_NAME=ghatd
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -a -installsuffix cgo -ldflags="-w -s" -o ./$BINARY_NAME main.go
```

##### Linux (ARM64)

```sh
export BINARY_NAME=ghatd
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -a -installsuffix cgo -ldflags="-w -s" -o ./$BINARY_NAME main.go
```

##### Linux (AMD64)

```sh
export BINARY_NAME=ghatd
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -ldflags="-w -s" -o ./$BINARY_NAME main.go
```

## License
This project is licensed under the [MIT License](./LICENSE).
