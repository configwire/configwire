package migrations

// ConfigWire data foundation (SQUASHED BASELINE).
//
// This file squashes the pre-v0.2 history (1787875200 init + 1790000000..
// 1790000021 deltas, i.e. everything through v0.1.13 / v0.1.14-rc.1) into a
// single init. It was generated with `migrate collections` from a database
// migrated to the v0.1.x-latest schema, then stripped of system collections
// (_mfas, _otps, _externalAuths, _authOrigins, _superusers) and the
// PocketBase-scaffolded dev-only `users` collection. Only the 13 ConfigWire
// app collections remain: projects, environments, groups, flags, rules,
// releases, sdk_keys, events, experiments, event_daily, rate_settings,
// identity_salts, retention_settings.
//
// Notes:
//   - All API rules are nil (default-deny); PocketBase nil-rule semantics:
//     nil denies non-superusers ("" would be PUBLIC). Server code uses
//     superuser context which bypasses rules.
//   - Collection ids are stable cw_* values so relation targets resolve.
//   - releases rows are immutable enforced by hooks in main.go.
//   - flags.key shape enforced by hooks in main.go.
//   - experiments.variants JSON shape follows configwire/eval/eval.go.
//   - events carries userHash/anonId/country here; v0.2 migrations
//     (1790000022 drop_identity_userhash, 1790000023 rename_anonid_bucket)
//     reshape identity storage on top.
//
// Upgrade rule: databases below v0.1.13 must migrate to v0.1.13 FIRST, then
// to v0.2.x. Operators with a _migrations table referencing the deleted
// incremental files run `migrate history-sync` to prune missing entries.

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Stable collection ids so relation fields can reference targets and the
// v0.2 deltas can resolve collections by id.
const (
	colProjects          = "cw_projects"
	colEnvironments      = "cw_environments"
	colGroups            = "cw_groups"
	colFlags             = "cw_flags"
	colRules             = "cw_rules"
	colReleases          = "cw_releases"
	colSDKKeys           = "cw_sdk_keys"
	colEvents            = "cw_events"
	colExperiments       = "cw_experiments"
	colEventDaily        = "cw_event_daily"
	colRateSettings      = "cw_rate_settings"
	colIdentitySalts     = "cw_identity_salts"
	colRetentionSettings = "cw_retention_settings"
)

func init() {
	m.Register(func(app core.App) error {
		jsonData := `[
	{
		"createRule": null,
		"deleteRule": null,
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"help": "",
				"hidden": false,
				"id": "text3208210256",
				"max": 15,
				"min": 15,
				"name": "id",
				"pattern": "^[a-z0-9]+$",
				"presentable": false,
				"primaryKey": true,
				"required": true,
				"system": true,
				"type": "text"
			},
			{
				"autogeneratePattern": "",
				"help": "",
				"hidden": false,
				"id": "text3479234172",
				"max": 0,
				"min": 0,
				"name": "owner",
				"pattern": "",
				"presentable": false,
				"primaryKey": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"autogeneratePattern": "",
				"help": "",
				"hidden": false,
				"id": "text1579384326",
				"max": 0,
				"min": 0,
				"name": "name",
				"pattern": "",
				"presentable": true,
				"primaryKey": false,
				"required": true,
				"system": false,
				"type": "text"
			}
		],
		"id": "cw_projects",
		"indexes": [],
		"listRule": null,
		"name": "projects",
		"system": false,
		"type": "base",
		"updateRule": null,
		"viewRule": null
	},
	{
		"createRule": null,
		"deleteRule": null,
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"help": "",
				"hidden": false,
				"id": "text3208210256",
				"max": 15,
				"min": 15,
				"name": "id",
				"pattern": "^[a-z0-9]+$",
				"presentable": false,
				"primaryKey": true,
				"required": true,
				"system": true,
				"type": "text"
			},
			{
				"cascadeDelete": true,
				"collectionId": "cw_projects",
				"help": "",
				"hidden": false,
				"id": "relation800313582",
				"maxSelect": 1,
				"minSelect": 0,
				"name": "project",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "relation"
			},
			{
				"autogeneratePattern": "",
				"help": "",
				"hidden": false,
				"id": "text2560465762",
				"max": 0,
				"min": 0,
				"name": "slug",
				"pattern": "",
				"presentable": true,
				"primaryKey": false,
				"required": true,
				"system": false,
				"type": "text"
			},
			{
				"autogeneratePattern": "",
				"help": "",
				"hidden": false,
				"id": "text2045250480",
				"max": 0,
				"min": 0,
				"name": "sdkKeyPrefix",
				"pattern": "",
				"presentable": false,
				"primaryKey": false,
				"required": false,
				"system": false,
				"type": "text"
			}
		],
		"id": "cw_environments",
		"indexes": [
			"CREATE INDEX ` + "`" + `idx_environments_slug_project` + "`" + ` ON ` + "`" + `environments` + "`" + ` (slug, project)"
		],
		"listRule": null,
		"name": "environments",
		"system": false,
		"type": "base",
		"updateRule": null,
		"viewRule": null
	},
	{
		"createRule": null,
		"deleteRule": null,
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"help": "",
				"hidden": false,
				"id": "text3208210256",
				"max": 15,
				"min": 15,
				"name": "id",
				"pattern": "^[a-z0-9]+$",
				"presentable": false,
				"primaryKey": true,
				"required": true,
				"system": true,
				"type": "text"
			},
			{
				"autogeneratePattern": "",
				"help": "",
				"hidden": false,
				"id": "text1579384326",
				"max": 0,
				"min": 0,
				"name": "name",
				"pattern": "",
				"presentable": true,
				"primaryKey": false,
				"required": true,
				"system": false,
				"type": "text"
			},
			{
				"cascadeDelete": true,
				"collectionId": "cw_projects",
				"help": "",
				"hidden": false,
				"id": "relation800313582",
				"maxSelect": 1,
				"minSelect": 0,
				"name": "project",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "relation"
			}
		],
		"id": "cw_groups",
		"indexes": [],
		"listRule": null,
		"name": "groups",
		"system": false,
		"type": "base",
		"updateRule": null,
		"viewRule": null
	},
	{
		"createRule": null,
		"deleteRule": null,
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"help": "",
				"hidden": false,
				"id": "text3208210256",
				"max": 15,
				"min": 15,
				"name": "id",
				"pattern": "^[a-z0-9]+$",
				"presentable": false,
				"primaryKey": true,
				"required": true,
				"system": true,
				"type": "text"
			},
			{
				"autogeneratePattern": "",
				"help": "",
				"hidden": false,
				"id": "text2324736937",
				"max": 128,
				"min": 1,
				"name": "key",
				"pattern": "^[A-Za-z_][A-Za-z0-9_.-]*$",
				"presentable": false,
				"primaryKey": false,
				"required": true,
				"system": false,
				"type": "text"
			},
			{
				"help": "",
				"hidden": false,
				"id": "select2363381545",
				"maxSelect": 1,
				"name": "type",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "select",
				"values": [
					"number",
					"string",
					"bool",
					"json"
				]
			},
			{
				"cascadeDelete": false,
				"collectionId": "cw_groups",
				"help": "",
				"hidden": false,
				"id": "relation1841317061",
				"maxSelect": 1,
				"minSelect": 0,
				"name": "group",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "relation"
			},
			{
				"autogeneratePattern": "",
				"help": "",
				"hidden": false,
				"id": "text1843675174",
				"max": 1024,
				"min": 0,
				"name": "description",
				"pattern": "",
				"presentable": false,
				"primaryKey": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"help": "",
				"hidden": false,
				"id": "json3294772269",
				"maxSize": 0,
				"name": "defaultValue",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "json"
			},
			{
				"cascadeDelete": true,
				"collectionId": "cw_projects",
				"help": "",
				"hidden": false,
				"id": "relation800313582",
				"maxSelect": 1,
				"minSelect": 0,
				"name": "project",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "relation"
			}
		],
		"id": "cw_flags",
		"indexes": [
			"CREATE UNIQUE INDEX ` + "`" + `idx_flags_key_project` + "`" + ` ON ` + "`" + `flags` + "`" + ` (key, project)"
		],
		"listRule": null,
		"name": "flags",
		"system": false,
		"type": "base",
		"updateRule": null,
		"viewRule": null
	},
	{
		"createRule": null,
		"deleteRule": null,
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"help": "",
				"hidden": false,
				"id": "text3208210256",
				"max": 15,
				"min": 15,
				"name": "id",
				"pattern": "^[a-z0-9]+$",
				"presentable": false,
				"primaryKey": true,
				"required": true,
				"system": true,
				"type": "text"
			},
			{
				"cascadeDelete": true,
				"collectionId": "cw_flags",
				"help": "",
				"hidden": false,
				"id": "relation3522489242",
				"maxSelect": 1,
				"minSelect": 0,
				"name": "flag",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "relation"
			},
			{
				"help": "",
				"hidden": false,
				"id": "number1655102503",
				"max": null,
				"min": null,
				"name": "priority",
				"onlyInt": true,
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"help": "",
				"hidden": false,
				"id": "json3184953411",
				"maxSize": 0,
				"name": "condition",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "json"
			},
			{
				"help": "",
				"hidden": false,
				"id": "json494360628",
				"maxSize": 0,
				"name": "value",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "json"
			}
		],
		"id": "cw_rules",
		"indexes": [
			"CREATE INDEX ` + "`" + `idx_rules_flag` + "`" + ` ON ` + "`" + `rules` + "`" + ` (flag)"
		],
		"listRule": null,
		"name": "rules",
		"system": false,
		"type": "base",
		"updateRule": null,
		"viewRule": null
	},
	{
		"createRule": null,
		"deleteRule": null,
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"help": "",
				"hidden": false,
				"id": "text3208210256",
				"max": 15,
				"min": 15,
				"name": "id",
				"pattern": "^[a-z0-9]+$",
				"presentable": false,
				"primaryKey": true,
				"required": true,
				"system": true,
				"type": "text"
			},
			{
				"help": "",
				"hidden": false,
				"id": "number3206337475",
				"max": null,
				"min": null,
				"name": "version",
				"onlyInt": true,
				"presentable": false,
				"required": true,
				"system": false,
				"type": "number"
			},
			{
				"autogeneratePattern": "",
				"help": "",
				"hidden": false,
				"id": "text3514087100",
				"max": 0,
				"min": 0,
				"name": "etag",
				"pattern": "",
				"presentable": false,
				"primaryKey": false,
				"required": true,
				"system": false,
				"type": "text"
			},
			{
				"help": "",
				"hidden": false,
				"id": "json743249205",
				"maxSize": 0,
				"name": "snapshot",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "json"
			},
			{
				"autogeneratePattern": "",
				"help": "",
				"hidden": false,
				"id": "text3182418120",
				"max": 0,
				"min": 0,
				"name": "author",
				"pattern": "",
				"presentable": false,
				"primaryKey": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"autogeneratePattern": "",
				"help": "",
				"hidden": false,
				"id": "text3485334036",
				"max": 0,
				"min": 0,
				"name": "note",
				"pattern": "",
				"presentable": false,
				"primaryKey": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"cascadeDelete": true,
				"collectionId": "cw_environments",
				"help": "",
				"hidden": false,
				"id": "relation4081402617",
				"maxSelect": 1,
				"minSelect": 0,
				"name": "env",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "relation"
			}
		],
		"id": "cw_releases",
		"indexes": [
			"CREATE UNIQUE INDEX ` + "`" + `idx_releases_env_version` + "`" + ` ON ` + "`" + `releases` + "`" + ` (env, version)"
		],
		"listRule": null,
		"name": "releases",
		"system": false,
		"type": "base",
		"updateRule": null,
		"viewRule": null
	},
	{
		"createRule": null,
		"deleteRule": null,
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"help": "",
				"hidden": false,
				"id": "text3208210256",
				"max": 15,
				"min": 15,
				"name": "id",
				"pattern": "^[a-z0-9]+$",
				"presentable": false,
				"primaryKey": true,
				"required": true,
				"system": true,
				"type": "text"
			},
			{
				"autogeneratePattern": "",
				"help": "",
				"hidden": false,
				"id": "text2477885070",
				"max": 0,
				"min": 0,
				"name": "prefix",
				"pattern": "",
				"presentable": false,
				"primaryKey": false,
				"required": true,
				"system": false,
				"type": "text"
			},
			{
				"autogeneratePattern": "",
				"help": "",
				"hidden": false,
				"id": "text3518522040",
				"max": 0,
				"min": 0,
				"name": "hash",
				"pattern": "",
				"presentable": false,
				"primaryKey": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"cascadeDelete": true,
				"collectionId": "cw_environments",
				"help": "",
				"hidden": false,
				"id": "relation4081402617",
				"maxSelect": 1,
				"minSelect": 0,
				"name": "env",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "relation"
			},
			{
				"help": "",
				"hidden": false,
				"id": "bool3181538509",
				"name": "revoked",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "bool"
			},
			{
				"help": "",
				"hidden": false,
				"id": "number477496688",
				"max": null,
				"min": null,
				"name": "rateLimit",
				"onlyInt": true,
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"help": "",
				"hidden": false,
				"id": "number2475698975",
				"max": null,
				"min": null,
				"name": "fetchRps",
				"onlyInt": true,
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"help": "",
				"hidden": false,
				"id": "number379876128",
				"max": null,
				"min": null,
				"name": "ingestRps",
				"onlyInt": true,
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"autogeneratePattern": "",
				"help": "",
				"hidden": false,
				"id": "text4220602952",
				"max": 0,
				"min": 0,
				"name": "verifier",
				"pattern": "",
				"presentable": false,
				"primaryKey": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"help": "",
				"hidden": false,
				"id": "number1520735457",
				"max": null,
				"min": null,
				"name": "keyVer",
				"onlyInt": true,
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			}
		],
		"id": "cw_sdk_keys",
		"indexes": [
			"CREATE INDEX ` + "`" + `idx_sdk_keys_prefix` + "`" + ` ON ` + "`" + `sdk_keys` + "`" + ` (prefix)"
		],
		"listRule": null,
		"name": "sdk_keys",
		"system": false,
		"type": "base",
		"updateRule": null,
		"viewRule": null
	},
	{
		"createRule": null,
		"deleteRule": null,
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"help": "",
				"hidden": false,
				"id": "text3208210256",
				"max": 15,
				"min": 15,
				"name": "id",
				"pattern": "^[a-z0-9]+$",
				"presentable": false,
				"primaryKey": true,
				"required": true,
				"system": true,
				"type": "text"
			},
			{
				"cascadeDelete": true,
				"collectionId": "cw_environments",
				"help": "",
				"hidden": false,
				"id": "relation4081402617",
				"maxSelect": 1,
				"minSelect": 0,
				"name": "env",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "relation"
			},
			{
				"autogeneratePattern": "",
				"help": "",
				"hidden": false,
				"id": "text4047749037",
				"max": 0,
				"min": 0,
				"name": "variant",
				"pattern": "",
				"presentable": false,
				"primaryKey": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"help": "",
				"hidden": false,
				"id": "select1002749145",
				"maxSelect": 1,
				"name": "kind",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "select",
				"values": [
					"fetch",
					"exposure"
				]
			},
			{
				"autogeneratePattern": "",
				"help": "",
				"hidden": false,
				"id": "text3623335415",
				"max": 0,
				"min": 0,
				"name": "userHash",
				"pattern": "",
				"presentable": false,
				"primaryKey": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"help": "",
				"hidden": false,
				"id": "number3206337475",
				"max": null,
				"min": null,
				"name": "version",
				"onlyInt": true,
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"help": "",
				"hidden": false,
				"id": "date3280375435",
				"max": "",
				"min": "",
				"name": "ts",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "date"
			},
			{
				"cascadeDelete": true,
				"collectionId": "cw_flags",
				"help": "",
				"hidden": false,
				"id": "relation3522489242",
				"maxSelect": 1,
				"minSelect": 0,
				"name": "flag",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "relation"
			},
			{
				"autogeneratePattern": "",
				"help": "",
				"hidden": false,
				"id": "text503374850",
				"max": 0,
				"min": 0,
				"name": "anonId",
				"pattern": "",
				"presentable": false,
				"primaryKey": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"autogeneratePattern": "",
				"help": "",
				"hidden": false,
				"id": "text1400097126",
				"max": 0,
				"min": 0,
				"name": "country",
				"pattern": "",
				"presentable": false,
				"primaryKey": false,
				"required": false,
				"system": false,
				"type": "text"
			}
		],
		"id": "cw_events",
		"indexes": [
			"CREATE INDEX ` + "`" + `idx_events_env_ts` + "`" + ` ON ` + "`" + `events` + "`" + ` (env, ts)",
			"CREATE INDEX ` + "`" + `idx_events_env_flag_ts` + "`" + ` ON ` + "`" + `events` + "`" + ` (env, flag, ts)"
		],
		"listRule": null,
		"name": "events",
		"system": false,
		"type": "base",
		"updateRule": null,
		"viewRule": null
	},
	{
		"createRule": null,
		"deleteRule": null,
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"help": "",
				"hidden": false,
				"id": "text3208210256",
				"max": 15,
				"min": 15,
				"name": "id",
				"pattern": "^[a-z0-9]+$",
				"presentable": false,
				"primaryKey": true,
				"required": true,
				"system": true,
				"type": "text"
			},
			{
				"autogeneratePattern": "",
				"help": "",
				"hidden": false,
				"id": "text1579384326",
				"max": 0,
				"min": 0,
				"name": "name",
				"pattern": "",
				"presentable": true,
				"primaryKey": false,
				"required": true,
				"system": false,
				"type": "text"
			},
			{
				"cascadeDelete": true,
				"collectionId": "cw_flags",
				"help": "",
				"hidden": false,
				"id": "relation3522489242",
				"maxSelect": 1,
				"minSelect": 0,
				"name": "flag",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "relation"
			},
			{
				"autogeneratePattern": "",
				"help": "",
				"hidden": false,
				"id": "text1149756166",
				"max": 0,
				"min": 0,
				"name": "seed",
				"pattern": "",
				"presentable": false,
				"primaryKey": false,
				"required": true,
				"system": false,
				"type": "text"
			},
			{
				"help": "T3 contract shape: [{name, weightBps (sum 10000), values}]",
				"hidden": false,
				"id": "json3013104609",
				"maxSize": 0,
				"name": "variants",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "json"
			},
			{
				"help": "",
				"hidden": false,
				"id": "select2063623452",
				"maxSelect": 1,
				"name": "status",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "select",
				"values": [
					"draft",
					"running",
					"stopped"
				]
			}
		],
		"id": "cw_experiments",
		"indexes": [
			"CREATE INDEX ` + "`" + `idx_experiments_flag` + "`" + ` ON ` + "`" + `experiments` + "`" + ` (flag)"
		],
		"listRule": null,
		"name": "experiments",
		"system": false,
		"type": "base",
		"updateRule": null,
		"viewRule": null
	},
	{
		"createRule": null,
		"deleteRule": null,
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"help": "",
				"hidden": false,
				"id": "text3208210256",
				"max": 15,
				"min": 15,
				"name": "id",
				"pattern": "^[a-z0-9]+$",
				"presentable": false,
				"primaryKey": true,
				"required": true,
				"system": true,
				"type": "text"
			},
			{
				"help": "",
				"hidden": false,
				"id": "date3852478864",
				"max": "",
				"min": "",
				"name": "day",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "date"
			},
			{
				"cascadeDelete": true,
				"collectionId": "cw_environments",
				"help": "",
				"hidden": false,
				"id": "relation4081402617",
				"maxSelect": 1,
				"minSelect": 0,
				"name": "env",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "relation"
			},
			{
				"autogeneratePattern": "",
				"help": "",
				"hidden": false,
				"id": "text4047749037",
				"max": 0,
				"min": 0,
				"name": "variant",
				"pattern": "",
				"presentable": false,
				"primaryKey": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"help": "",
				"hidden": false,
				"id": "number3206337475",
				"max": null,
				"min": null,
				"name": "version",
				"onlyInt": true,
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"help": "",
				"hidden": false,
				"id": "number2679112534",
				"max": null,
				"min": null,
				"name": "fetches",
				"onlyInt": true,
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"help": "",
				"hidden": false,
				"id": "number4276289071",
				"max": null,
				"min": null,
				"name": "exposures",
				"onlyInt": true,
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"cascadeDelete": true,
				"collectionId": "cw_flags",
				"help": "",
				"hidden": false,
				"id": "relation3522489242",
				"maxSelect": 1,
				"minSelect": 0,
				"name": "flag",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "relation"
			}
		],
		"id": "cw_event_daily",
		"indexes": [
			"CREATE INDEX ` + "`" + `idx_event_daily_day` + "`" + ` ON ` + "`" + `event_daily` + "`" + ` (day)",
			"CREATE UNIQUE INDEX ` + "`" + `idx_event_daily_upsert_key` + "`" + ` ON ` + "`" + `event_daily` + "`" + ` (day, env, flag, variant, version)"
		],
		"listRule": null,
		"name": "event_daily",
		"system": false,
		"type": "base",
		"updateRule": null,
		"viewRule": null
	},
	{
		"createRule": null,
		"deleteRule": null,
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"help": "",
				"hidden": false,
				"id": "text3208210256",
				"max": 15,
				"min": 15,
				"name": "id",
				"pattern": "^[a-z0-9]+$",
				"presentable": false,
				"primaryKey": true,
				"required": true,
				"system": true,
				"type": "text"
			},
			{
				"autogeneratePattern": "",
				"help": "",
				"hidden": false,
				"id": "text2324736937",
				"max": 0,
				"min": 0,
				"name": "key",
				"pattern": "",
				"presentable": true,
				"primaryKey": false,
				"required": true,
				"system": false,
				"type": "text"
			},
			{
				"help": "",
				"hidden": false,
				"id": "number1309097052",
				"max": null,
				"min": null,
				"name": "rps",
				"onlyInt": true,
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"help": "",
				"hidden": false,
				"id": "number2101256368",
				"max": null,
				"min": null,
				"name": "burst",
				"onlyInt": true,
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"help": "",
				"hidden": false,
				"id": "number2475698975",
				"max": null,
				"min": null,
				"name": "fetchRps",
				"onlyInt": true,
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"help": "",
				"hidden": false,
				"id": "number379876128",
				"max": null,
				"min": null,
				"name": "ingestRps",
				"onlyInt": true,
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"help": "",
				"hidden": false,
				"id": "number1948269980",
				"max": null,
				"min": null,
				"name": "adminRps",
				"onlyInt": true,
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"autogeneratePattern": "",
				"help": "",
				"hidden": false,
				"id": "text525015974",
				"max": 0,
				"min": 0,
				"name": "ipHeaders",
				"pattern": "",
				"presentable": false,
				"primaryKey": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"autogeneratePattern": "",
				"help": "",
				"hidden": false,
				"id": "text2302227714",
				"max": 0,
				"min": 0,
				"name": "adminAllowedIPs",
				"pattern": "",
				"presentable": false,
				"primaryKey": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"autogeneratePattern": "",
				"help": "",
				"hidden": false,
				"id": "text2180348649",
				"max": 0,
				"min": 0,
				"name": "countryHeaders",
				"pattern": "",
				"presentable": false,
				"primaryKey": false,
				"required": false,
				"system": false,
				"type": "text"
			}
		],
		"id": "cw_rate_settings",
		"indexes": [
			"CREATE UNIQUE INDEX ` + "`" + `idx_rate_settings_key` + "`" + ` ON ` + "`" + `rate_settings` + "`" + ` (key)"
		],
		"listRule": null,
		"name": "rate_settings",
		"system": false,
		"type": "base",
		"updateRule": null,
		"viewRule": null
	},
	{
		"createRule": null,
		"deleteRule": null,
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"help": "",
				"hidden": false,
				"id": "text3208210256",
				"max": 15,
				"min": 15,
				"name": "id",
				"pattern": "^[a-z0-9]+$",
				"presentable": false,
				"primaryKey": true,
				"required": true,
				"system": true,
				"type": "text"
			},
			{
				"autogeneratePattern": "",
				"help": "",
				"hidden": false,
				"id": "text1283451447",
				"max": 0,
				"min": 0,
				"name": "envID",
				"pattern": "",
				"presentable": true,
				"primaryKey": false,
				"required": true,
				"system": false,
				"type": "text"
			},
			{
				"autogeneratePattern": "",
				"help": "",
				"hidden": false,
				"id": "text3852478864",
				"max": 0,
				"min": 0,
				"name": "day",
				"pattern": "",
				"presentable": true,
				"primaryKey": false,
				"required": true,
				"system": false,
				"type": "text"
			},
			{
				"autogeneratePattern": "",
				"help": "",
				"hidden": false,
				"id": "text2415649015",
				"max": 0,
				"min": 0,
				"name": "salt",
				"pattern": "",
				"presentable": false,
				"primaryKey": false,
				"required": true,
				"system": false,
				"type": "text"
			}
		],
		"id": "cw_identity_salts",
		"indexes": [
			"CREATE UNIQUE INDEX ` + "`" + `idx_identity_salts_env_day` + "`" + ` ON ` + "`" + `identity_salts` + "`" + ` (envID, day)"
		],
		"listRule": null,
		"name": "identity_salts",
		"system": false,
		"type": "base",
		"updateRule": null,
		"viewRule": null
	},
	{
		"createRule": null,
		"deleteRule": null,
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"help": "",
				"hidden": false,
				"id": "text3208210256",
				"max": 15,
				"min": 15,
				"name": "id",
				"pattern": "^[a-z0-9]+$",
				"presentable": false,
				"primaryKey": true,
				"required": true,
				"system": true,
				"type": "text"
			},
			{
				"autogeneratePattern": "",
				"help": "",
				"hidden": false,
				"id": "text2324736937",
				"max": 0,
				"min": 0,
				"name": "key",
				"pattern": "",
				"presentable": true,
				"primaryKey": false,
				"required": true,
				"system": false,
				"type": "text"
			},
			{
				"help": "",
				"hidden": false,
				"id": "number72577560",
				"max": null,
				"min": null,
				"name": "rawDays",
				"onlyInt": true,
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"help": "",
				"hidden": false,
				"id": "number2433814586",
				"max": null,
				"min": null,
				"name": "rollupDays",
				"onlyInt": true,
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			}
		],
		"id": "cw_retention_settings",
		"indexes": [
			"CREATE UNIQUE INDEX ` + "`" + `idx_retention_settings_key` + "`" + ` ON ` + "`" + `retention_settings` + "`" + ` (key)"
		],
		"listRule": null,
		"name": "retention_settings",
		"system": false,
		"type": "base",
		"updateRule": null,
		"viewRule": null
	}
]`

		return app.ImportCollectionsByMarshaledJSON([]byte(jsonData), false)
	}, func(app core.App) error {
		// Reverse dependency order so relation targets drop last.
		for _, name := range []string{
			"experiments",
			"event_daily",
			"events",
			"sdk_keys",
			"releases",
			"rules",
			"flags",
			"groups",
			"environments",
			"projects",
			"rate_settings",
			"identity_salts",
			"retention_settings",
		} {
			collection, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				continue // already gone; keep down idempotent
			}
			if err := app.Delete(collection); err != nil {
				return err
			}
		}
		return nil
	})
}
