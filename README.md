# Template Golang HTMX Alpine Tailwind

- **template of:** golang + htmx + alpine + tailwind
- **htmx:** [v1.9.10](https://htmx.org/)
- **alpine.js:** [v3.x](https://alpinejs.dev/essentials/installation#from-a-script-tag)
- **tailwindcss:** [v3.x](https://github.com/asdf-community/asdf-golang)
- **version manager:** [asdf]([text](https://github.com/asdf-community/asdf-golang))


## Starting the server

To start the server you can use the code:

```sh
go run main.go start-server
```

> This template application will be referred to as `ghat` (Go HTMX Alpine Tailwind) throughout the template, use this to replace the name

## Good to know

### ASCI Art

All ASCI related code in this template was created using [PatorJK](https://patorjk.com/software/taag/#p=display&h=2&f=Isometric3)

### Core internal packages

Some core packages are used across the codebase without injection; they include:

- `internal/response`
- `internal/router`
- `internal/toolbox`

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
