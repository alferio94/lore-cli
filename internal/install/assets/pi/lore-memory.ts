const loreBinaryPath = "{{LORE_BINARY_PATH}}";
const loreConfigDir = "{{LORE_CONFIG_DIR}}";
const loreServerURL = "{{LORE_SERVER_URL}}";

function brokerArgs(method: string, path: string, body?: unknown) {
  const args = ["api", "request", "--json", "--method", method, "--path", path];
  if (body !== undefined) {
    args.push("--body-json", JSON.stringify(body));
  }
  return {
    command: loreBinaryPath,
    env: { LORE_CONFIG_DIR: loreConfigDir },
    args,
    serverURL: loreServerURL,
  };
}

export const lore_search = { name: "lore_search", broker: () => brokerArgs("GET", "/v1/search") };
export const lore_save = { name: "lore_save", broker: (body?: unknown) => brokerArgs("POST", "/v1/observations", body) };
export const lore_update = { name: "lore_update", broker: (id: string, body?: unknown) => brokerArgs("PATCH", `/v1/observations/${id}`, body) };
export const lore_delete = { name: "lore_delete", broker: (id: string) => brokerArgs("DELETE", `/v1/observations/${id}`) };
export const lore_get_observation = { name: "lore_get_observation", broker: (id: string) => brokerArgs("GET", `/v1/observations/${id}`) };
export const lore_context = { name: "lore_context", broker: () => brokerArgs("GET", "/v1/context") };
export const lore_timeline = { name: "lore_timeline", broker: () => brokerArgs("GET", "/v1/timeline") };
export const lore_stats = { name: "lore_stats", broker: () => brokerArgs("GET", "/v1/stats") };
export const lore_session_summary = { name: "lore_session_summary", broker: (body?: unknown) => brokerArgs("POST", "/v1/sessions", body) };

// Broker contract: lore api request --json --method <METHOD> --path <PATH> [--body-json <JSON>]
