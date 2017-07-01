# doc-checker

`doc-checker` can be run to ensure that all your terraform provider documentation is up to date.

It checks that:

- all data sources have a documentation page
- all resources have a documentation page

## usage

```bash
$ go get -u github.com/nicolai86/terraform-tools/cmd/doc-checker
$ doc-checker -provider-name scaleway \
  -provider-path ~/go/src/github.com/terraform-providers/terraform-provider-scaleway/scaleway/
```

## TODO

- [ ] check for doc structure
- [ ] check that docs only contain valid attributes (e.g. find typos, no longer existing attributes)
- [ ] check deprecated fields are removed
