import { i } from "@instantdb/core";

// In Darkened Dreams character builder — cloud data model.
//
// "Private libraries only": every character belongs to exactly one signed-in
// user (via the `owner` link to the built-in $users entity), and the permission
// rules (instant.perms.ts) let a user see and edit only their own characters.
//
// `data` holds the full build JSON the app already produces (buildJSON()); the
// scalar columns (name/heritage/faction/valid/cp*) are denormalized copies so
// the library list can render + sort without parsing every blob.
const _schema = i.schema({
  entities: {
    // $users is Instant's built-in auth entity; declared here so the owner link
    // resolves. email is the system-managed attribute.
    $users: i.entity({
      email: i.string().unique().indexed(),
    }),
    characters: i.entity({
      name: i.string(),
      data: i.json(), // the app's buildJSON() output — the source of truth
      heritage: i.string().optional(),
      faction: i.string().optional(),
      valid: i.boolean(), // passes the rules validator (no errors)
      cpSpent: i.number(),
      cpAvailable: i.number(),
      createdAt: i.number().indexed(),
      updatedAt: i.number().indexed(),
    }),
  },
  links: {
    characterOwner: {
      forward: { on: "characters", has: "one", label: "owner" },
      reverse: { on: "$users", has: "many", label: "characters" },
    },
  },
});

type _AppSchema = typeof _schema;
interface AppSchema extends _AppSchema {}
const schema: AppSchema = _schema;

export type { AppSchema };
export default schema;
