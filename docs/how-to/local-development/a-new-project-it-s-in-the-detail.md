# Getting Started With A New Project: It's in the Details


With [**`Details`**](../../about-details.md), you can transform your ideas and hobbies into real, tangible products in no time! These building blocks are designed to help you demo and even ship your creations faster than ever.

## Prerequisites

- Go installed/ available
- The [**`GHAT(D) CLI`**](#) installed

## Steps

- Discover the power of the [Detail Library](#). It's a good place to find the foundation for your project.
  
- Generate a new web app based on your chosen **detail(s)** (`GHAT(D)` gives you the option to merge multiple details into one web app (feature still in alpha)). In the terminal, run:


```shell
ghatdcli new -n [PROJECT_NAME] -m [PROJECT_MODULE_PATH] -w [COMMA_SEPARATED_LINK_TO_DETAIL(S)] -o [DESTINATION_DIR]
```

> Remember to replace the placeholders in the command above! 
>
> - It is possible to ignore`[DESTINATION_DIR]` if you want the new app to be generated in the same folder `ghatdcli` is being used in.

- Run your new web app:

```shell
cd [PROJECT_NAME]
go mod tidy
go run main.go start-server
```

> For the best developer experience we recommend using [`reflex`](https://github.com/cespare/reflex).

## Additional context



