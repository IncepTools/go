# blog

A database-backed service. SQLite, so there is nothing to install, but the
code is the code you would run against PostgreSQL — change the dialector
and the migration's types and nothing else moves.

```bash
go run ./blog

ACC=11111111-1111-1111-1111-111111111111
USR=22222222-2222-2222-2222-222222222222
H=(-H "Content-Type: application/json" -H "X-Account-Id: $ACC" -H "X-User-Id: $USR")

ID=$(curl -s -X POST localhost:8081/api/v1/blog/posts "${H[@]}" \
     -d '{"title":"Hello, world!","body":"First post."}' | jq -r .data.id)

curl -X POST "localhost:8081/api/v1/blog/posts/$ID/publish" "${H[@]}"
# a second later the relay logs: delivered outbox message
```

## Things worth trying

| Try this | And see |
|---|---|
| Create the same title twice | 409 `SLUG_TAKEN` — and a unique index behind it |
| `{"title":"X","slug":"Not A Slug"}` | A custom `slug` rule, with its own message |
| Omit `slug` | Derived from the title by `SetDefaults()` |
| Publish twice | 409 `ALREADY_PUBLISHED`, decided under a row lock |
| List as a different account | `[]` — the repository filtered it, not the caller |
| Delete the `.db` file and restart | Migrations re-apply from scratch |

## The two things this example exists for

### The ownership filter is not the caller's job

```go
repo.New[entity.Post](db,
	repo.WithEntityName("post"),
	repo.Tenanted(),
	repo.WithSortable(constants.Sortable...),
)
```

Every read is filtered by account and a query with no account is refused.
Nothing in `service/` writes a `WHERE account_id = ?`, which is why none of
it can forget to.

### The state change and its side effect are one transaction

```go
err := repo.Transaction(ctx, s.db, func(ctx context.Context) error {
	post, err := s.repo.LockByID(ctx, id)   // FOR UPDATE
	...
	if err := s.repo.UpdateByID(ctx, id, fields); err != nil {
		return err
	}
	return outbox.Enqueue(ctx, s.db, constants.TopicPublished, event)
})
```

Publishing after the commit means a crash in between leaves a published
post nothing downstream knows about. Publishing before means an event for a
post that does not exist. There is no ordering of two systems that fixes
that, so the message goes in the same transaction — and `Enqueue` refuses
to run outside one.

The relay then delivers it, retrying with backoff, and parks anything that
exhausts its attempts rather than deleting it.

## Where to look

| | |
|---|---|
| [`migrations/`](migrations) | Versioned SQL, including the index that makes the duplicate check true |
| [`entity/post.go`](apps/blog/posts/entity/post.go) | The stored shape, pinned to the table |
| [`repository/`](apps/blog/posts/repository) | Four lines, because the generic repository does the rest |
| [`service/post_service.go`](apps/blog/posts/service/post_service.go) | The transaction, the row lock, the outbox |
| [`main.go`](main.go) | The relay, the custom validation rule, the health check |

## Schema

`icpt` manages it. This example applies the SQL itself at start-up so that
`go run` is the only command you need — a shortcut for the example, not a
pattern to copy. A real service runs:

```bash
export BLOG_DATABASE_URL=/tmp/blog.db
icpt migrate
icpt status
```

See [`icpt.json`](icpt.json).
