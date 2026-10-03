# Contributing

1. Fork the repository and create a branch.
2. Keep each directory to one job. Keep each file under 300 lines.
3. Run `gofmt -l .` (it must print nothing), `go vet ./...`, and `go test ./... -race`.
4. A bug fix adds one test named `TestRegression...` that fails before the fix.
5. A change to a tenant boundary adds a negative test: another tenant, a missing token, and the agent role.
6. Open a pull request. CI runs the Postgres, NATS, and Temporal integration tests.

The integration variables are in the README under Tests.
