module.exports = (schemas, paths, { ref, obj, str, arr, integer, route }) => {
  const mapping = {
    allOf: [
      ref("Binding"),
      { properties: { kind: { enum: ["field", "constant"] } } },
    ],
  };
  schemas.DeliveryBinding = obj({
    id: str({ pattern: "^[a-z][a-z0-9_]{0,63}$" }),
    port: str({ pattern: "^[a-z][a-z0-9_]{0,63}$" }),
    plugin_code: { const: "aria2" },
    version: { const: "co.delivery.aria2.v1" },
    emit: { enum: ["valid", "created", "changed"] },
    mapping: {
      type: "object",
      required: ["url"],
      minProperties: 1,
      maxProperties: 2,
      properties: { url: ref("Binding"), dir: ref("Binding") },
      additionalProperties: false,
    },
  });
  schemas.DeliveryBinding.properties.mapping.properties = {
    url: mapping,
    dir: mapping,
  };
  schemas.Stage.properties.deliveries = arr(ref("DeliveryBinding"), 5);
  schemas.DeliveryControlRequest = obj({
    delivery_id: str({ minLength: 1 }),
    action: { enum: ["query", "retry", "confirm_delivered", "cancel"] },
    expected_revision: integer(1, 2147483647),
    confirmed: { const: true },
  });
  for (const [method, url, name, request] of [
    ["get", "/delivery-capabilities", "getCoDeliveryCapabilities"],
    ["get", "/runs/{run_id}/deliveries", "listCoDeliveries"],
    [
      "post",
      "/runs/{run_id}/deliveries/control",
      "controlCoDelivery",
      "DeliveryControlRequest",
    ],
  ]) {
    route(method, url, name, "O5", request);
    paths[url][method]["x-implementation-status"] = "implemented";
  }
  paths["/runs/{run_id}/deliveries"].get.description =
    "Owner-scoped cursor page. Snapshot and outbox commit with persisted valid output; trial previews never call external service. Fixed output+binding idempotency, frozen aria2 contract and destination fingerprint, private credentials remain in service config. Configuration changes block recovery until original target is restored or manually confirmed. One worker, 30s remote budget, 90s database-clock lease; expired sending only queries original GID. Success means accepted, not download completion. Collection and delivery status are independent. At most5 bindings per stage,10000 active deliveries per owner; payload and reserved8KiB receipt count toward output storage quota.";
  paths["/runs/{run_id}/deliveries/control"].post.description =
    "Confirmed revision CAS and operation idempotency. Query an unknown original request; manually confirm acceptance or end tracking without cancelling external action. Retry only known unsubmitted failures without external ID. Unknown actions are never automatically resubmitted. Run pause blocks queued sends; cancellation cancels queued sends, sent requests keep original receipt reconciliation.";
};
