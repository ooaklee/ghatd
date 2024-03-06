# GHATD - An opinionated, Go-based, full-stack web application template

GHATD stands for Go, HTMX, Alpine.js, Tailwind, and DaisyUI. It is a top-quality, opinionated, open-source, and free full-stack web application template written in Go programming language. GHATD is perfect for creating highly portable, scalable, and performant backend, landing pages, web applications, and full projects.

See below for more information on the core components used for this stack.

- **go:** [v1.22.x](https://go.dev/doc/install)
- **htmx:** [v1.9.10](https://htmx.org/)
- **alpine.js:** [v3.x](https://alpinejs.dev/essentials/installation#from-a-script-tag)
- **tailwindcss:** [v3.x](https://github.com/asdf-community/asdf-golang)
- **daisy ui:** [v3.x](https://daisyui.com/docs/install/)
  -  Notable alternatives include:
    - **flowbite:** [v2.3.x](https://flowbite.com/docs/getting-started/introduction/#include-via-cdn)
- **version manager:** [asdf](https://github.com/asdf-vm/asdf)

> The dashboard's base template was taken from the `TailAdmin team`. Please support them by [**purchasing their templates**](https://tailwindadmin.netlify.app/) or giving their [**GitHub repository**](https://github.com/TailAdmin/tailadmin-free-tailwind-dashboard-template) a star.
>
> 
> This template application will be referred to as `ghatd` (**Go**, **HTMX**, **Alpine JS**, **Tailwind CSS** &  **Daisy UI**) throughout the template, for information on the list of variables you should replace after cloning this repo, [vist this section](#remember-to-replace)

## Motivation

GHATD is a hobby project I am working on in my spare time. The project aims to give those like me who like Go and want to work on projects that sometimes require a frontend a good "getting started" template that we can leverage to build out other projects and initiatives.

I also aim to use this project to improve my understanding of and share my knowledge of lightweight frontend libraries, highly portable full-stack alternatives, and cost-effective infrastructure solutions for full-scale products. 

As I develop this project, I will create tutorials/guides for those who want to integrate it with other technologies, such as `rpc`,  `graphql`,  `websocket`, and others.

I am a platform engineer by trade, so I do not promise perfect code by any stretch of the imagination (especially with the front end - so please support and contribute). Instead, I want to create a standardised template that helps those curious (about Go, APIs, hubby projects, and web app development) turn their ideas/ hobbies into tangible product(s) that they can demo to friends, family or colleagues and even ship.

This will be an exciting experience, and I look forward to developing this project and sharing my progress and knowledge as it matures.

### A little more about me

Engineering, tech, learning and helping others grow are some of my passions; I often find myself reading, listening or watching something related to the engineering world, micro saas, or latest advancements and every so often, I like to dive deeper into a topic and create a project/integrate into an existing project to see what takeaways I can take back to my work or future hobby projects, which in the past led to me toying with a plethora of stacks and deployment methods, including but not limited to serverless infrastructure, containerised service, bundled static HTML including immutable web apps, Go backend, Python applications (Flask & Django), vue-based frameworks (Vue.js, Nuxt, etc.), react-based frameworks (React, Next.js), Flutter frontends, Chrome extension (using Vue.js & React), pure HTML, Wordpress based frontend implementation (PHP), building micro frontends and more.

Ultimately, I've grown to accept that I want to work with Go first and foremost for my hobby projects. I don't particularly appreciate repeating feature implementations across multiple codebases, nor am I a fan of the constant context-switching between the frontend code base, backend, etc. Instead, I want one base template to rule them all. I know this has the potential to be disastrous. Still, it will reduce fatigue by consolidating many of the repetitious areas/ pain points I dreaded when working on some of my past solo projects and help me continue my journey of discovering Go.


## Starting the server

Before getting started please mak sure you have the correct version of [Go installed](https://go.dev/doc/install) or you can use [ASDF](https://github.com/asdf-vm/asdf) to install it with the following command

```sh
# Add the plugin for Go
asdf plugin-add golang

# Install required version
asdf install
```

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

### Remember to replcae

After you have cloned this repository, please make sure to replace or update the following:

- `tbc`

### ASCI Art

All ASCI related code in this template was created using [PatorJK](https://patorjk.com/software/taag/#p=display&h=2&f=Isometric3)

### Core internal packages

Some core internal packages are used across the codebase without injection; they include:

- `internal/response`
- `internal/router`
- `internal/toolbox`
- `internal/common`

### Curl Examples

- Making `GET` resquest with query param: `curl -i -X GET "http://localhost:4000/snippet/view?id=2"`

### How to stop file server showing directory listing?

Add a blank index.html file to the specific directory that you want to disable listings for. For example, the
code below will create an index file which will stop [the webapp](http://localhost:4000/static/) from showing 
and listing page.

```sh
touch internal/webapp/ui/static/index.html
```

### Hot reloading

Install reflex

`go install github.com/cespare/reflex@latest`

> You can find more information in the repo https://github.com/cespare/reflex

Once installed, run the server

```
reflex -r '\.(html|go|css|png|svg|ico|js|woff2|woff|ttf|eot)$' -s -- go run main.go start-server
```

### How to build binaries

One of the benefits of using the GHATD stack is that it compiles everything into a single binary. This makes it highly portable and provides numerous deployment options. To build a binary for your specific system, please follow the instructions below:

> All commands should be executed from the root directory.

#### Mac OS (ARM64)

```sh
export BINARY_NAME=ghatd
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -a -installsuffix cgo -ldflags="-w -s" -o ./$BINARY_NAME main.go
```

#### Mac OS (AMD64)

```sh
export BINARY_NAME=ghatd
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -a -installsuffix cgo -ldflags="-w -s" -o ./$BINARY_NAME main.go
```

#### Linux (ARM64)

```sh
export BINARY_NAME=ghatd
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -a -installsuffix cgo -ldflags="-w -s" -o ./$BINARY_NAME main.go
```

#### Linux (AMD64)

```sh
export BINARY_NAME=ghatd
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -ldflags="-w -s" -o ./$BINARY_NAME main.go
```

## License
This project is licensed under the [MIT License](./LICENSE).
