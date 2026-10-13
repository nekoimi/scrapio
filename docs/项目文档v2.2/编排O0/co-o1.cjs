// O1 implemented contracts extend the frozen O0 definitions. O2+ remain planned.
module.exports = function (schemas, paths, helpers) {
  const { ref, obj, str, integer, arr, route } = helpers;
  schemas.Error.properties.error.properties.details = ref("BoundedObject");
  schemas.Pagination.properties.cursor_parameter = str({
    pattern: "^[a-z][a-z0-9_]{0,63}$",
  });
  schemas.Pagination.properties.empty_marker = ref("Locator");
  schemas.DocumentSnapshot = obj(
    {
      url: str(),
      format: { enum: ["html", "json"] },
      content: str({ maxLength: 262144 }),
      page_state_id: str(),
    },
    ["url", "format", "content"],
  );
  schemas.SampleRequest = obj(
    {
      expected_revision: integer(0, 2147483647),
      evidence_id: str({ minLength: 1 }),
      input: ref("BoundedObject"),
      snapshots: arr(ref("DocumentSnapshot"), 3, 1),
      expected_outputs: arr(ref("BoundedObject"), 100),
    },
    ["expected_revision"],
    {
      oneOf: [
        {
          required: ["evidence_id"],
          not: {
            anyOf: [{ required: ["input"] }, { required: ["snapshots"] }],
          },
        },
        {
          required: ["input", "snapshots"],
          not: { required: ["evidence_id"] },
        },
      ],
    },
  );
  schemas.Operation = obj(
    {
      id: str(),
      resource_id: str(),
      operation: str(),
      status: {
        enum: ["queued", "running", "succeeded", "failed", "cancelled"],
      },
      result: ref("BoundedObject"),
      error: ref("BoundedObject"),
      created_at: str(),
      finished_at: { type: ["string", "null"] },
    },
    [
      "id",
      "resource_id",
      "operation",
      "status",
      "result",
      "error",
      "created_at",
    ],
  );
  schemas.CollectorView = obj(
    {
      id: str(),
      name: str(),
      revision: integer(0, 2147483647),
      archived: { type: "boolean" },
      definition: { oneOf: [ref("CollectorDefinition"), obj({}, [])] },
      created_at: str(),
      updated_at: str(),
      version_number: integer(0, 100000),
      latest_test: ref("BoundedObject"),
    },
    [
      "id",
      "name",
      "revision",
      "archived",
      "definition",
      "created_at",
      "updated_at",
    ],
  );
  schemas.Candidate = obj(
    {
      identity: str(),
      values: ref("BoundedObject"),
      valid: { type: "boolean" },
      page: integer(0, 100),
      fields: {
        type: "object",
        additionalProperties: obj(
          {
            match_count: integer(0, 100000),
            status: { enum: ["valid", "invalid", "missing"] },
            message: str(),
          },
          ["match_count", "status"],
        ),
      },
    },
    ["values", "valid", "page", "fields"],
  );
  schemas.TestResult = obj(
    {
      status: {
        enum: [
          "succeeded",
          "partial",
          "limited",
          "failed",
          "awaiting_confirmation",
        ],
      },
      mode: { enum: ["live", "offline"] },
      reason: str(),
      interpreter_version: { const: "co.interpreter.v1" },
      definition_hash: str(),
      input: ref("BoundedObject"),
      candidates: arr(ref("Candidate"), 100),
      pages: arr(
        obj({
          url: str(),
          content_hash: str(),
          record_count: integer(0, 10000),
          empty_verified: { type: "boolean" },
        }),
        3,
      ),
      checkpoint: ref("BoundedObject"),
      warnings: arr(str(), 100),
      error: ref("BoundedObject"),
      snapshots: arr(ref("DocumentSnapshot"), 3),
      comparison: ref("BoundedObject"),
      test_limits: ref("BoundedObject"),
      formal_writes: { const: 0 },
    },
    [
      "status",
      "mode",
      "interpreter_version",
      "definition_hash",
      "input",
      "candidates",
      "pages",
      "checkpoint",
      "warnings",
      "formal_writes",
    ],
  );
  schemas.SampleView = obj({
    id: str(),
    revision: integer(0, 2147483647),
    input: ref("BoundedObject"),
    snapshots: arr(ref("DocumentSnapshot"), 3),
    expected_outputs: arr(ref("BoundedObject"), 100),
    created_at: str(),
  });
  schemas.CollectorVersionView = obj({
    id: str(),
    number: integer(1, 100000),
    revision: integer(1, 2147483647),
    definition: ref("CollectorDefinition"),
    definition_hash: str(),
    input_schema_hash: str(),
    output_schema_hash: str(),
    interpreter_version: { const: "co.interpreter.v1" },
    publish_version: { const: "co.publish.v1" },
    requirements: ref("BoundedObject"),
    evidence_id: str(),
    created_at: str(),
  });
  schemas.SessionView = obj({
    session_id: str(),
    collector_id: str(),
    draft_revision: integer(1, 2147483647),
    status: str(),
    expires_at: str(),
    page_state_id: str(),
    current_url: str(),
    viewport: obj({ width: integer(1, 2560), height: integer(1, 1600) }),
    frame_url: str(),
    available: { type: "boolean" },
    needs_reopen: { type: "boolean" },
    state: { type: ["object", "null"] },
  });
  schemas.SessionEnvelope = obj({
    data: ref("SessionView"),
    request_id: str(),
  });
  for (const name of ["Collector", "Sample", "CollectorVersion"]) {
    schemas[name + "Envelope"] = obj({
      data: ref(name + "View"),
      request_id: str(),
    });
    schemas[name + "ListEnvelope"] = obj({
      data: obj({ items: arr(ref(name + "View"), 100), next_cursor: str() }),
      request_id: str(),
    });
  }
  schemas.TestListEnvelope = obj({
    data: obj({ items: arr(ref("Operation"), 100), next_cursor: str() }),
    request_id: str(),
  });
  schemas.ArchiveRequest = obj({ expected_revision: integer(0, 2147483647) });
  route(
    "get",
    "/collectors/{collector_id}/tests",
    "listCoTests",
    "O1",
    undefined,
    "TestListEnvelope",
  );
  route(
    "post",
    "/collectors/{collector_id}/archive",
    "archiveCoCollector",
    "O1",
    "ArchiveRequest",
    "CollectorEnvelope",
  );
  route("delete", "/operations/{operation_id}", "deleteCoTest", "O1");
  route("get", "/operations/{operation_id}/receipt", "getCoTestReceipt", "O1");
  route("delete", "/samples/{sample_id}", "deleteCoSample", "O1");
  route("delete", "/sessions/{session_id}", "closeCoEditorSession", "O1");
  for (const [url, method, response] of [
    ["/collectors", "get", "CollectorListEnvelope"],
    ["/collectors", "post", "CollectorEnvelope"],
    ["/collectors/{collector_id}", "get", "CollectorEnvelope"],
    ["/collectors/{collector_id}/draft", "get", "CollectorEnvelope"],
    ["/collectors/{collector_id}/draft", "put", "CollectorEnvelope"],
    ["/collectors/{collector_id}/samples", "get", "SampleListEnvelope"],
    ["/collectors/{collector_id}/samples", "post", "SampleEnvelope"],
    [
      "/collectors/{collector_id}/versions",
      "get",
      "CollectorVersionListEnvelope",
    ],
    ["/collectors/{collector_id}/versions", "post", "CollectorVersionEnvelope"],
    ["/sessions/{session_id}", "get", "SessionEnvelope"],
  ])
    paths[url][method].responses["200"].content["application/json"].schema =
      ref(response);
  for (const [url, item] of Object.entries(paths))
    for (const [method, op] of Object.entries(item)) {
      if (op["x-delivery-phase"] !== "O1") continue;
      op["x-implementation-status"] = "implemented";
      op.responses["404"] = { description: "Missing resource or owner denied" };
      op.responses["503"] = { description: "Required capability unavailable" };
      if (method === "delete")
        for (const p of op.parameters)
          if (p.name === "Idempotency-Key") p.required = false;
    }
  paths["/sessions/{session_id}/frame"].get.parameters.push({
    name: "page_state_id",
    in: "query",
    required: true,
    schema: str({ minLength: 1 }),
  });
  paths["/operations/by-key"].get.parameters.push({
    name: "resource_id",
    in: "query",
    required: true,
    schema: str({
      description: "Collector/session ID; empty for createCollector",
    }),
  });
  paths["/sessions/{session_id}/commands"].post.description =
    "command_id must equal Idempotency-Key. An unresolved prior command blocks a new action; no automatic replay.";
};
