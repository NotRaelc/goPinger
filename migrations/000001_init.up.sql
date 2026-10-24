CREATE TABLE IF NOT EXISTS users(
                       username VARCHAR(32) NOT NULL,
                       hash VARCHAR NOT NULL,
                       token VARCHAR,
                       token_expires_at TIMESTAMPTZ,

                       UNIQUE(username),
                       UNIQUE(hash),
                       UNIQUE(token)
);
/*
CREATE TABLE ServerTypes (
    ip VARCHAR NOT NULL,
    port INTEGER NOT NULL,
    game_kind VARCHAR NOT NULL,

    UNIQUE(ip, port)
);

CREATE TABLE ServerCache (
    game_kind VARCHAR NOT NULL,
    online BOOLEAN,
    name VARCHAR,
    game_id INTEGER,
    protocol INTEGER,
    versions ...,
    ip VARCHAR NOT NULL,
    port INTEGER,
    hostname VARCHAR,
    players ...,
    online_players INTEGER,
    max_players INTEGER,
    icon VARCHAR,
    software VARCHAR,
    gamemode VARCHAR,
    gamemode_id INTEGER,
    server_id VARCHAR,
    eula_blocked BOOLEAN,
    motd ...,
    plugins ...,
    mods ...,
    info ...,
    keywords ...,
    debug ...
)
 */
/*
	Versions []string `json:"versions"`
    Players       []Player `json:"players"`

    MOTD        *MOTD      `json:"motd,omitempty"`
    Plugins     []Plugin   `json:"plugins,omitempty"`
    Mods        []Mod      `json:"mods,omitempty"`
    Info        *MOTD      `json:"info,omitempty"`
    Keywords    any        `json:"keywords,omitempty"`
    Debug       *DebugInfo `json:"debug,omitempty"`

 */