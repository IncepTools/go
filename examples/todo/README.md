# todo

An in-memory CRUD service that exercises most of the framework without a
database.

```bash
go run ./todo

ACC=11111111-1111-1111-1111-111111111111
USR=22222222-2222-2222-2222-222222222222
H=(-H "Content-Type: application/json" -H "X-Account-Id: $ACC" -H "X-User-Id: $USR")

curl -X POST localhost:8080/api/v1/todo/tasks "${H[@]}" \
     -d '{"title":"Write the docs","priority":"high","tags":["  Writing  "]}'

curl "localhost:8080/api/v1/todo/tasks?sort=title" "${H[@]}"
curl -X POST localhost:8080/api/v1/todo/tasks/<id>/complete "${H[@]}"
```

## Things worth trying

| Try this | And see |
|---|---|
| Omit the headers on a `POST` | 401 `UNAUTHENTICATED` — a module-wide guard |
| `{"title":"ab"}` | 400 with a field-scoped message naming `title` |
| `{"title":"X","due_at":"2020-01-01T00:00:00Z"}` | The cross-field rule in `Validate()` |
| Omit `priority` | It comes back `"normal"` — `SetDefaults()` |
| Send a tag with spaces and capitals | It is stored trimmed and lowercased — `Normalize()` |
| `?sort=password` | 400 `INVALID_SORT`, listing what is allowed |
| `?limit=100000` | Clamped to the ceiling, not refused |
| Complete a task | An async subscriber logs it, off the request path |

## Where to look

| | |
|---|---|
| [`dto/dto.go`](apps/todo/tasks/dto/dto.go) | The three hooks, and why updates use pointers |
| [`service/service.go`](apps/todo/tasks/service/service.go) | Business rules with no HTTP import |
| [`controller/controller.go`](apps/todo/tasks/controller/controller.go) | Routes as values, with guards and docs attached |
| [`tasks_module.go`](apps/todo/tasks/tasks_module.go) | Providers, the event subscription and the scheduled job |
| [`main.go`](main.go) | Assembly, and the identity resolver you must not copy |

Next: [blog](../blog) is the same shape with a database behind it.
