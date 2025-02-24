# mustache-codegen

mustache-codegen is an implementation of the [Mustache templating language][]
that emits compiled code for Go and JavaScript.
This permits programs to use static Mustache templates
for rendering text formats like HTML
without requiring the runtime overhead and binary size of template parsing.

mustache-codegen implements Mustache v1.4, including [inheritance][].
See [mustache(5)][] for a syntax manual.

[Mustache templating language]: https://mustache.github.io/
[inheritance]: https://mustache.github.io/mustache.5.html#Parents
[mustache(5)]: https://mustache.github.io/mustache.5.html

## Installation

[Install Go][], and then run:

```shell
go install github.com/kagisearch/mustache-codegen/cmd/mustache-codegen@latest
```

This will install the `mustache-codegen` in `$GOPATH/bin`.
(Use `go env GOPATH` to locate `$GOPATH` if this is not already on your `PATH`.)

[Install Go]: https://go.dev/dl/

## Using with Go

Use `mustache-codegen -lang=go` to generate Go code from a Mustache template.
For a template named "foo_bar.mustache",
mustache-codegen will create a function:

```go
func FooBar(buf *bytes.Buffer, data any)
```

The data is accessed via reflection.
See the [support package][Go support package] for details on how Mustache tags
map to Go data structures.

Here's a "Hello World" example:

`foo.mustache`

```mustache
Hello, {{foo}}!
```

Compile the template with `mustache-codegen -lang=go -o foo.go foo.mustache`, then you can use it like:

`main.go`

```go
package main

import (
	"bytes"
	"os"
)

func main() {
	buf := new(bytes.Buffer)
	Foo(buf, map[string]string{"foo": "World"})
	buf.WriteTo(os.Stdout)
}
```

The generated package name can be changed with the `-go-package` option.

[Go support package]: https://pkg.go.dev/github.com/kagisearch/mustache-codegen/go/mustache

## Using with JavaScript

Use `mustache-codegen -lang=js` to generate JavaScript code from a Mustache template.
The generated code will use [JavaScript module syntax][]
and export a default function that takes the data as its sole argument
and returns the rendered template as a string.

Here's a "Hello World" example with Node.js:

`foo.mustache`

```mustache
Hello, {{foo}}!
```

Compile the template with `mustache-codegen -lang=js -o foo.mjs foo.mustache`, and then use it like:

`main.js`

```js
import foo from "./foo.mjs"

console.log(foo({foo: "World"}))
```

And run it with `node --input-type=module main.js`

[JavaScript module syntax]: https://developer.mozilla.org/en-US/docs/Web/JavaScript/Guide/Modules

### Using with TypeScript

If you are using [TypeScript][],
you can make a `.js` file typed by adding a [`.d.ts` file][] with the same base name.
For the example above, a reasonable `foo.d.ts` file would be:

```typescript
export interface Params {
  foo: string
}
export default function(params: Params): string
```

mustache-codegen cannot automatically generate type information for its templates
because Mustache tags can intentionally operate on many data types
and variables can refer to data in parent contexts.

[TypeScript]: https://www.typescriptlang.org/
[`.d.ts` file]: https://www.typescriptlang.org/docs/handbook/declaration-files/introduction.html

## Partials

Upon encountering a partial tag like `{{>foo}}` or a parent tag like `{{<foo}}`,
mustache-codegen will look for a `foo.mustache` file
in the same directory as the template it appears in
(or in the current working directory, if the template is being read from stdin).

## License

[MIT](LICENSE)
