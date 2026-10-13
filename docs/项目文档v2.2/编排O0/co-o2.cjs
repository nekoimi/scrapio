// Actual O2 single-stage API; O3 graph and O4 sources remain planned.
module.exports = (schemas, paths, { ref, obj, str, integer, arr, route }) => {
  // O0 was a proposal. Replace the planned O2 surface with mounted endpoints;
  // previews are synchronous and trials use the same published task/run model.
  for (const [url, methods] of Object.entries(paths)) {
    for (const [method, op] of Object.entries(methods))
      if (op["x-delivery-phase"] === "O2") delete methods[method];
    if (!Object.keys(methods).length) delete paths[url];
  }
  schemas.CollectorVersionView.properties.collector_id = str();
  schemas.SingleStageTaskDefinition = {
    allOf: [
      ref("TaskDefinition"),
      {
        properties: {
          stages: { minItems: 1, maxItems: 1 },
          edges: { maxItems: 0 },
          source: {
            properties: { kind: { const: "manual" } },
            required: ["inputs"],
          },
        },
      },
    ],
  };
  schemas.TaskCreateRequest = obj({
    name: str({ minLength: 1, maxLength: 64 }),
    definition: ref("SingleStageTaskDefinition"),
  });
  schemas.TaskDraftWrite = obj({
    expected_revision: integer(0, 2147483647),
    definition: ref("SingleStageTaskDefinition"),
  });
  schemas.TaskPublishRequest = obj({
    expected_revision: integer(1, 2147483647),
  });
  schemas.TableSchema = obj({
    fields: arr(ref("Field"), 30, 1),
    unique_key: arr(str(), 5, 1),
  });
  schemas.TableCreateRequest = obj({
    name: str({ minLength: 1, maxLength: 64 }),
    schema: ref("TableSchema"),
  });
  schemas.TaskInputPreviewRequest = obj({
    collector_version_id: str(),
    inputs: arr(ref("BoundedObject"), 100, 1),
  });
  schemas.TaskSavePreviewRequest = obj({
    collector_version_id: str(),
    save: ref("SaveBinding"),
    candidates: arr(ref("Candidate"), 100),
  });
  schemas.TaskRunRequest = obj({
    task_version_id: str(),
    namespace: { enum: ["formal", "trial"] },
  });
  schemas.FailedRetryRequest = obj({ confirmed: { const: true } });
  schemas.ItemResolutionRequest = obj({
    action: { enum: ["fail", "restart"] },
    confirmed: { const: true },
  });
  schemas.TaskRunStatus = {
    enum: [
      "preparing",
      "running",
      "pausing",
      "paused",
      "awaiting_confirmation",
      "succeeded",
      "partial",
      "limited",
      "failed",
      "cancelled",
    ],
  };
  schemas.TaskView = obj({
    id: str(),
    name: str(),
    revision: integer(1, 2147483647),
    definition: ref("SingleStageTaskDefinition"),
    created_at: str(),
    updated_at: str(),
  });
  schemas.TaskEnvelope = obj({ data: ref("TaskView"), request_id: str() });
  const add = (method, url, id, request, response) =>
    route(method, url, id, "O2", request, response || "Envelope");
  add(
    "get",
    "/collector-versions/{version_id}",
    "getCoCollectorVersion",
    undefined,
    "CollectorVersionEnvelope",
  );
  add("get", "/tasks", "listCoTasks");
  add("post", "/tasks", "createCoTask", "TaskCreateRequest", "TaskEnvelope");
  add("get", "/tasks/{task_id}", "getCoTask", undefined, "TaskEnvelope");
  add(
    "get",
    "/tasks/{task_id}/draft",
    "getCoTaskDraft",
    undefined,
    "TaskEnvelope",
  );
  add(
    "put",
    "/tasks/{task_id}/draft",
    "saveCoTaskDraft",
    "TaskDraftWrite",
    "TaskEnvelope",
  );
  add("get", "/tasks/{task_id}/versions", "listCoTaskVersions");
  add(
    "post",
    "/tasks/{task_id}/versions",
    "publishCoTask",
    "TaskPublishRequest",
  );
  add(
    "post",
    "/tasks/input-preview",
    "previewCoTaskInputs",
    "TaskInputPreviewRequest",
  );
  add(
    "post",
    "/tasks/save-preview",
    "previewCoTaskSave",
    "TaskSavePreviewRequest",
  );
  add("post", "/tasks/{task_id}/runs", "startCoTaskRun", "TaskRunRequest");
  add("get", "/tables", "listCoTaskTables");
  add("post", "/tables", "createCoTaskTable", "TableCreateRequest");
  add("get", "/tables/{table_id}", "getCoTaskTable");
  add("get", "/tables/{table_id}/records", "listCoTaskRecords");
  add("get", "/records/{record_id}", "getCoTaskRecordHistory");
  add("get", "/runs", "listCoTaskRuns");
  add("get", "/runs/{run_id}", "getCoTaskRun");
  paths["/runs/{run_id}"].get.parameters.push({
    name: "summary",
    in: "query",
    schema: { enum: ["1"] },
    description: "Status/counters only; omit frozen manifest during polling.",
  });
  for (const resource of ["work-items", "outputs", "observations"])
    add(
      "get",
      "/runs/{run_id}/" + resource,
      "listCoTaskRun" + resource.replace(/-./g, (x) => x[1].toUpperCase()),
    );
  add("get", "/runs/{run_id}/events", "listCoTaskRunEvents");
  paths["/runs/{run_id}/events"].get.parameters = paths[
    "/runs/{run_id}/events"
  ].get.parameters.filter((p) => !["cursor", "limit"].includes(p.name));
  paths["/runs/{run_id}/events"].get.parameters.push({
    name: "after",
    in: "query",
    schema: integer(0, Number.MAX_SAFE_INTEGER),
  });
  paths["/runs/{run_id}/events"].get.description =
    "Bounded ordered JSON polling, at most 100 metadata events; next_sequence resumes after disconnect. No SSE dependency or automatic trigger.";
  add("post", "/runs/{run_id}/control", "controlCoTaskRun", "ControlRequest");
  add(
    "post",
    "/runs/{run_id}/retry-failed",
    "retryCoTaskFailedInputs",
    "FailedRetryRequest",
  );
  add("get", "/runs/{run_id}/work-items/{item_id}", "getCoTaskWorkItem");
  add(
    "post",
    "/runs/{run_id}/work-items/{item_id}/resolve",
    "resolveCoTaskWorkItem",
    "ItemResolutionRequest",
  );
  add(
    "get",
    "/runs/{run_id}/work-items/{item_id}/attempts/{attempt_id}/commands/{command_id}",
    "getCoTaskOriginalReceipt",
  );
  for (const methods of Object.values(paths))
    for (const op of Object.values(methods))
      if (op["x-delivery-phase"] === "O2") {
        op["x-implementation-status"] = "implemented";
        op.responses["404"] = {
          description: "Missing resource or owner denied",
        };
        op.responses["503"] = {
          description: "Required capability unavailable",
        };
      }
};
