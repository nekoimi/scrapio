// Mounted O3 graph extensions. O4 input sources and scoped retries remain planned.
module.exports = (schemas, paths, { ref, obj, str, integer, arr, route }) => {
  for (const [url, methods] of Object.entries(paths)) {
    for (const [method, op] of Object.entries(methods))
      if (op["x-delivery-phase"] === "O3") delete methods[method];
    if (!Object.keys(methods).length) delete paths[url];
  }
  schemas.EdgeBinding = {
    oneOf: [
      obj({
        kind: { const: "field" },
        path: str({
          pattern:
            "^(output\\.[a-z][a-z0-9_]*|input\\.[a-z][a-z0-9_]*|element)$",
        }),
      }),
      obj({ kind: { const: "constant" }, value: {} }),
      obj({ kind: { const: "template" }, value: str({ maxLength: 2048 }) }),
    ],
  };
  schemas.EdgeFilter = obj(
    {
      path: str({
        pattern: "^(output\\.[a-z][a-z0-9_]*|input\\.[a-z][a-z0-9_]*|element)$",
      }),
      op: { enum: ["exists", "equal", "not_equal"] },
      value: {},
    },
    ["path", "op"],
  );
  schemas.Edge.properties.mapping.additionalProperties = ref("EdgeBinding");
  schemas.Edge.properties.filter = ref("EdgeFilter");
  schemas.TaskDefinition.properties.stages.maxItems = 20;
  schemas.TaskDefinition.properties.edges.maxItems = 19;
  schemas.TaskDefinition.properties.budget.properties.max_dispatches = integer(
    1,
    10000,
  );
  schemas.GraphTaskDefinition = {
    allOf: [
      ref("TaskDefinition"),
      {
        properties: {
          source: {
            properties: { kind: { const: "manual" } },
            required: ["inputs"],
          },
        },
      },
    ],
  };
  schemas.TaskCreateRequest.properties.definition = ref("GraphTaskDefinition");
  schemas.TaskDraftWrite.properties.definition = ref("GraphTaskDefinition");
  schemas.TrialOptions = obj(
    {
      mode: { enum: ["live", "offline"] },
      documents: {
        type: "object",
        maxProperties: 20,
        additionalProperties: arr(
          obj({
            url: str(),
            format: { enum: ["html", "json"] },
            content: str({ maxLength: 524288 }),
          }),
          20,
        ),
      },
    },
    ["mode"],
  );
  schemas.TaskRunRequest.properties.trial = ref("TrialOptions");
  schemas.TaskMappingPreviewRequest = obj({
    upstream_version_id: str(),
    downstream_version_id: str(),
    edge: ref("Edge"),
    input: ref("BoundedObject"),
    output: ref("BoundedObject"),
  });
  route(
    "post",
    "/tasks/mapping-preview",
    "previewCoTaskMapping",
    "O3",
    "TaskMappingPreviewRequest",
  );
  route("get", "/runs/{run_id}/dispatches", "listCoTaskDispatches", "O3");
  paths["/runs/{run_id}/dispatches"].get.parameters.push(
    { name: "cursor", in: "query", schema: str() },
    { name: "limit", in: "query", schema: integer(1, 100) },
  );
  for (const methods of Object.values(paths))
    for (const op of Object.values(methods))
      if (op["x-delivery-phase"] === "O3")
        op["x-implementation-status"] = "implemented";
  paths["/runs/{run_id}/work-items/{item_id}"].get.description =
    "Frozen input, attempts and bounded recursive lineage (up to 20 upstream stages).";
  paths["/runs/{run_id}"].get.description =
    "Run and per-stage status, input/work counts, immutable outputs, save decisions, filtered/delivered/remaining dispatches and pause reason.";
  paths["/tasks/{task_id}/runs"].post.description =
    "Published frozen graph; formal writes or isolated trial. Trials clamp work to 100, outputs to 500, pages/item to 3 and total duration to 120 seconds. Offline documents are immutable run snapshots; missing evidence is reported. Request <=1MiB, documents <=768KiB.";
  paths["/runs/{run_id}/retry-failed"].post.description =
    "Single-stage original-version failed-only replay. Multi-stage scoped replay returns SCOPED_RETRY_REQUIRED until O4.";
};
