CREATE TABLE IF NOT EXISTS logs (
    time DateTime,
    agent LowCardinality(String),
    source LowCardinality(String),
    level LowCardinality(String),
    ip String,
    status UInt16,
    message String
) ENGINE = MergeTree
PARTITION BY toDate(time)
ORDER BY (source, agent, time);
