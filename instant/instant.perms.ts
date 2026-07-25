import type { InstantRules } from "@instantdb/core";

// Owner-only access: a character is visible and editable ONLY by the signed-in
// user it is linked to (the `owner` link → $users). `data.ref('owner.id')`
// returns the linked user id(s), so we test membership with `in`. The `create`
// rule runs inside the transaction and validates that the new character is
// linked to the current user — so nobody can write into someone else's library.
//
// Logged-out visitors can still use the builder offline (import/export/share
// link); they simply can't read or write the cloud library.
const rules = {
  characters: {
    allow: {
      view: "auth.id in data.ref('owner.id')",
      create: "auth.id in data.ref('owner.id')",
      update: "auth.id in data.ref('owner.id')",
      delete: "auth.id in data.ref('owner.id')",
    },
  },
} satisfies InstantRules;

export default rules;
