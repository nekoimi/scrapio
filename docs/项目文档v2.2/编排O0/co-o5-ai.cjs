module.exports = (schemas, paths, { ref, obj, str, integer, route }) => {
  schemas.AIPrepareRequest = obj(
    {
      kind: { enum: ["selectors", "mapping", "failure"] },
      resource_id: str({ minLength: 1 }),
      expected_revision: integer(0, 2147483647),
      sample_id: str({ minLength: 1 }),
      port: str(),
      edge_id: str(),
      output_index: integer(0, 99),
      item_id: str(),
    },
    ["kind", "resource_id", "expected_revision"],
  );
  schemas.AIControlRequest = obj(
    {
      confirmed: { const: true },
      expected_revision: integer(0, 2147483647),
      evidence_hash: str({ minLength: 1 }),
    },
    ["confirmed"],
  );
  schemas.AICleanupRequest = obj({ confirmed: { const: true } });
  for (const [method, url, name, request] of [
    ["get", "/ai-capabilities", "getCoAICapabilities"],
    ["post", "/ai/prepare", "prepareAIAssist", "AIPrepareRequest"],
    ["get", "/ai/suggestions", "listCoAISuggestions"],
    ["get", "/ai/suggestions/{suggestion_id}", "getCoAISuggestion"],
    [
      "delete",
      "/ai/suggestions/{suggestion_id}",
      "deleteAIAssist",
      "AICleanupRequest",
    ],
    [
      "post",
      "/ai/suggestions/{suggestion_id}/call",
      "callAIAssist",
      "AIControlRequest",
    ],
    ...["validate", "accept", "apply", "reject"].map((action) => [
      "post",
      "/ai/suggestions/{suggestion_id}/" + action,
      action + "AIAssist",
      "AIControlRequest",
    ]),
  ]) {
    route(method, url, name, "O5", request);
    paths[url][method]["x-implementation-status"] = "implemented";
  }
  paths["/ai/suggestions"].get.parameters.push(
    {
      name: "resource_kind",
      in: "query",
      required: true,
      schema: { enum: ["collector", "task", "run"] },
    },
    {
      name: "resource_id",
      in: "query",
      required: true,
      schema: str({ minLength: 1 }),
    },
  );
  paths["/ai/prepare"].post.description =
    "Explicit owner-scoped saved sample, port, edge/expected-output index, or failed work item. Freeze current revision/hash and the local full sample. Preview only the bounded, redacted model payload (24KiB), model, prompt version/hash and unknown monetary cost. No model call at this step. Mapping samples must match the published upstream revision; only scalar single/union paths, no expand/transforms/join/batch edits.";
  paths["/ai/suggestions/{suggestion_id}/call"].post.description =
    "Confirmed one-shot call bound to canonical prompt evidence_hash. Persist calling and original operation before network I/O outside database transaction. 20s timeout, 1000 output tokens, 64KiB gateway response; no redirects. Default 10 calls per owner/database calendar day, unknowns and failures count. A new key cannot resubmit calling/unknown/ready rows; query original suggestion/operation. Model output is untrusted and strictly decoded, then validated by the actual collector interpreter/task mapper against frozen samples.";
  paths["/ai/suggestions/{suggestion_id}/validate"].post.description =
    "Explicit confirmation of displayed validation evidence_hash; deterministically recheck the frozen candidate and sample. Sample-only evidence never substitutes for live publication evidence.";
  paths["/ai/suggestions/{suggestion_id}/accept"].post.description =
    "Separate explicit acceptance after confirmed validation; binds the accepted candidate, never edits configuration.";
  paths["/ai/suggestions/{suggestion_id}/apply"].post.description =
    "Requires accepted status, evidence_hash, expected_revision and unchanged original draft/hash/model/gateway/prompt version. Revalidate; update only existing element locators/extraction or declared scalar edge bindings in a transaction. Rebuild full task manifest before commit. Increment revision; require new tests/publication. Never changes schema, credentials, origins, actions, paging, published versions or active runs. Failure explanation is read-only and cannot apply.";
  paths["/ai/suggestions/{suggestion_id}"].delete.description =
    "Confirmed cleanup only for applied/rejected/failed suggestions. Clear frozen sample, prompt, candidate and duplicated prepare evidence and release reserved storage; retain status/called_at tombstone and original call identity so cleanup cannot reset daily quota. Unknown calls must first be explicitly ended. Existing adopted drafts are retained.";
};
