# EvalGate Go SDK

Official EvalGate REST client, generated from the same verified OpenAPI contract
as the TypeScript, Python, and CLI clients. Product: https://evalgate.com.

Requires Go 1.23 or newer. The module has no third-party dependencies.

```sh
go get github.com/evalgate/evalgate-go/v3@v3.15.0
```

```go
package main

import (
    "context"
    "log"
    "os"

    evalgate "github.com/evalgate/evalgate-go/v3"
)

func main() {
    client, err := evalgate.NewClient(evalgate.Config{
        APIKey: os.Getenv("EVALGATE_API_KEY"),
        OrganizationID: os.Getenv("EVALGATE_ORGANIZATION_ID"),
    })
    if err != nil { log.Fatal(err) }
    response, err := client.GetEvaluations(context.Background(), evalgate.GetEvaluationsParams{
        Query: evalgate.GetEvaluationsQuery{Limit: evalgate.Ptr[int64](10)},
    })
    if err != nil { log.Fatal(err) }
    log.Printf("HTTP %d; quota %s", response.StatusCode, response.Headers.Get("RateLimit"))
}
```

Generate an API key with the minimum required scopes, such as `eval:read` for
`GetEvaluations`. `Operations()` exposes every generated operation's method,
path, parameter requirements, pagination style, and required scopes. The server
enforces organization and scope authorization; the client does not expand them.

All operation methods have generated parameter and body types. Schema unions
without one Go representation retain `json.RawMessage`; explicit free-form
schemas retain `any` or maps. `generation.json` records the exact source digest,
version, and operation inventory. Generated schema models preserve the API's
field names in JSON tags.

An optional field adds a pointer for presence. Leave it `nil` to omit it, or use
`Ptr([]string{})` / `Ptr(map[string]JsonValue{})` to send an explicit empty
collection. Nullable fields add their own pointer: for an optional nullable
string such as a workflow description, `Ptr((*string)(nil))` sends JSON `null`,
and `Ptr(Ptr("updated"))` sends a string. This keeps omitted fields from clearing
existing values during a mutation. JSON integers retain their exact decimal
representation through the transport, including values larger than 2^53.
Generated model decoders retain omitted, null, and empty collection states when
reading and re-encoding JSON as well.

For a mutation, set `params.Options.IdempotencyKey` to a stable key when retrying
the same logical operation. The client never retries automatically or follows
redirects. Do not reuse the key for a different mutation.

`Response[T]` exposes parsed data, status, and all response headers. Use
`errors.As(err, &apiError)` with `*evalgate.APIError` to inspect status, structured
code, request ID, `RateLimit`, `RateLimit-Policy`, and `Retry-After`. A 429 should
be rescheduled according to those actual response headers. Pass context deadlines
or cancellation; the default HTTP timeout is 30 seconds. `Config.HTTPClient`
accepts a custom transport and timeout while redirect prevention stays enabled.

`client.Paginate(ctx, "get_evaluations", params, 100, visit)` visits raw JSON pages
using the generated cursor/offset/page contract. The explicit positive page bound,
repeated cursor detection, and no-progress check prevent an unattended agent
from looping forever. Returning an error from `visit` stops iteration.

Set `Config.BaseURL` to a disposable local EvalGate instance when testing writes.
The SDK does not create a hosted sandbox or substitute a fake production key.

To maintain this package in the platform source repository:

```sh
node scripts/generate-sdk-ecosystems.mjs
node scripts/generate-sdk-ecosystems.mjs --check
cd src/packages/sdk-go
go test ./...
go vet ./...
```

Generation requires Node 24 and `gofmt` from Go 1.23+. Public distribution contains
only this package, its license, and generated contract digest; the platform
source and private deployment configuration are not part of the module.

Documentation: https://www.evalgate.com/docs/sdk/go. Support: hello@evalgate.com.
