# About Details

**Details** are "builder blocks" which we introduced to reduce cognitive load and make hitting the floor running easier. A **`detail`** is a boilerplate independent application that can function both within the GHAT(D) framework and on their own. 

> At present, we only support `api` and `web` typed `Details`.

## Running Details Independently
All the **details** should be able to function independently, allowing users to work on them without considering other components. To get started, find the **detail** you need and clone it to your local machine. Depending on the type of detail you choose, as specified in the `ghatd-conf.yaml`, you should be able to run the equivalent of:

```shell
go run [DETAIL_TYPE].go
```

## Installing Details
By using the `ghatdcli new` command, it will handle cloning referenced **detail** boilerplate(s), configuring dependencies, and merging into consolidated ghat(d) web app.



