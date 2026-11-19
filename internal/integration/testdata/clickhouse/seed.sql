CREATE DATABASE g2 ENGINE = Atomic;
CREATE TABLE g2.sample UUID 'cb200000-0000-4000-8000-000000000001'
(id UInt32, title Nullable(String), secret Nullable(String)) ENGINE = MergeTree ORDER BY id;
INSERT INTO g2.sample VALUES (1, '研发', 'synthetic-private'), (2, NULL, NULL);
CREATE TABLE g2.large_cell UUID 'cb200000-0000-4000-8000-000000000002' AS g2.sample ENGINE = MergeTree ORDER BY id;
INSERT INTO g2.large_cell VALUES (1, 'ok', NULL), (2, repeat('x', 1025), NULL);
CREATE TABLE g2.large_reply UUID 'cb200000-0000-4000-8000-000000000003' AS g2.sample ENGINE = MergeTree ORDER BY id;
INSERT INTO g2.large_reply SELECT number, repeat(char(1), 950), NULL FROM numbers(12);
CREATE TABLE g2.slow UUID 'cb200000-0000-4000-8000-000000000004' AS g2.sample ENGINE = MergeTree ORDER BY id;
INSERT INTO g2.slow SELECT number, 'ok', NULL FROM numbers(20);
-- Fixture-controlled delay for observing an in-flight ordinary projection; no client SQL option.
CREATE ROW POLICY g2_delay ON g2.slow FOR SELECT USING sleepEachRow(0.135) = 0 TO ALL;
CREATE TABLE g2.late UUID 'cb200000-0000-4000-8000-000000000005' AS g2.sample
ENGINE = MergeTree PARTITION BY id ORDER BY id;
INSERT INTO g2.late SELECT number, 'ok', NULL FROM numbers(16);
-- Deterministic target-side fault on one synthetic part, after ordinary parts can be streamed.
CREATE ROW POLICY g2_late_error ON g2.late FOR SELECT USING throwIf(if(id=15, sleep(0.7) + 1, 0), 'g2 fixture read error')=0 TO ALL;
CREATE TABLE g2.server_budget UUID 'cb200000-0000-4000-8000-000000000006' AS g2.sample ENGINE = MergeTree ORDER BY id;
INSERT INTO g2.server_budget SELECT 1, repeat('abcd', 524288), NULL;

CREATE TABLE g2.events UUID 'cb200000-0000-4000-8000-000000000007'
(request_id UInt64, observed_at DateTime('Asia/Tokyo'), request_date Date, private_time DateTime, preview_only Date, missing_date Date)
ENGINE = MergeTree ORDER BY observed_at;
INSERT INTO g2.events
WITH toStartOfDay(now('UTC')) AS midnight
SELECT multiIf(number=0,toUInt64(42),number=1,toUInt64(43),toUInt64('18446744073709551615')),
       midnight + toIntervalSecond([-518401,-518400,-518399,-432001,-432000,0,toInt32(toUnixTimestamp(now())-toUnixTimestamp(midnight))+600,86400][number+1]) AS instant,
       toDate(instant, 'UTC'), instant, toDate(instant, 'UTC'), toDate(instant, 'UTC') FROM numbers(8);
