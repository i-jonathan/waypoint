CREATE TABLE waypoint_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
CREATE TABLE project (
    id       BIGINT PRIMARY KEY CHECK (id = 1),
    revision BIGINT NOT NULL,
    settings TEXT NOT NULL
);
CREATE TABLE board_columns (
    position BIGINT PRIMARY KEY,
    name     TEXT NOT NULL,
    wip      BIGINT NOT NULL,
    done     BIGINT NOT NULL,
    moves_to TEXT NOT NULL
);
CREATE TABLE board_groups (
    position BIGINT PRIMARY KEY,
    name     TEXT NOT NULL,
    color    TEXT NOT NULL
);
CREATE TABLE cards (
    id          TEXT PRIMARY KEY,
    position    BIGINT NOT NULL,
    column_name TEXT NOT NULL,
    title       TEXT NOT NULL,
    group_name  TEXT NOT NULL,
    priority    TEXT NOT NULL,
    depends     TEXT NOT NULL,
    created     TEXT NOT NULL,
    done        TEXT NOT NULL,
    body        TEXT NOT NULL,
    archived_at TEXT
);
CREATE TABLE card_fields (
    card_id     TEXT NOT NULL,
    position    BIGINT NOT NULL,
    field_key   TEXT NOT NULL,
    field_value TEXT NOT NULL,
    PRIMARY KEY (card_id, position)
);
