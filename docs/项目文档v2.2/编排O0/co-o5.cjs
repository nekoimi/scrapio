// Selected O5-01 extension; other optional O5 capabilities remain unimplemented.
module.exports = (schemas, paths, { str }) => {
  schemas.Stage.properties.input_policy = {
    enum: ["keep_all", "merge_identical"],
    description:
      "Optional; absent means keep_all. Merge only within one run and stage, using the complete normalized input and frozen collector version. All source snapshots retained. Shared outcome and one downstream delivery, including late sources.",
  };
  const item = paths["/runs/{run_id}/work-items/{item_id}"].get;
  item.parameters.push({
    name: "source_cursor",
    in: "query",
    schema: str({ maxLength: 36 }),
  });
  item.description =
    "Canonical input, attempts, sources (100 per page, next_source_cursor) and upstream lineage (20 stages, first100 relations, lineage_has_more). Each retained source has its own snapshot and merged receipt. Follow upstream items to inspect remaining ancestry.";
  paths["/runs/{run_id}"].get.description +=
    " Selected O5-01 adds per-stage input policy, source_count, actual input_count, merged_inputs and outgoing merged_dispatches. Merged is dispatch receipt status, never a synthetic work item or independent success.";
};
