# schema-checker

`schema-checker` can be run to ensure that all your terraform provider follows best practice

It checks that:

- all your resources attributes have a `Description` set
- you're not using reserved attribute names (like `id`)

## usage

```bash
$ go get -u github.com/nicolai86/terraform-tools/cmd/schema-checker
$ schema-checker -provider-path ~/go/src/github.com/terraform-providers/terraform-provider-scaleway/scaleway/
```
