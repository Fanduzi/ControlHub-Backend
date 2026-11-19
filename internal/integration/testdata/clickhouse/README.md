# ClickHouse preview integration fixture

`fixture.py` creates a private TLS identity, a transient data volume and a maintenance container. It loads `seed.sql`, stops the builder and starts the restricted reader container. Only Native TLS is published, on a random loopback port. The test owns the state directory and holds the permanent lease exclusively when simulating maintenance. Closing the fixture's stdin removes its containers and volume. The generated password travels over a private pipe to the Go test.

`seed.sql` contains synthetic data, UTC day-boundary events with a Tokyo timestamp and maximum UInt64 ID, and deterministic row-policy delays/errors used to distinguish confirmed database termination, partial streams and local cancellation. Run through `TestClickHousePreviewHTTP` as described in the [preview guide](../../../../docs/clickhouse-preview.md).
