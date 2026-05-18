const loreBinaryPath = "{{LORE_BINARY_PATH}}";
const loreConfigDir = "{{LORE_CONFIG_DIR}}";
const loreServerURL = "{{LORE_SERVER_URL}}";

export const lore_delegate = {
  name: "lore_delegate",
  broker: () => ({
    command: loreBinaryPath,
    env: { LORE_CONFIG_DIR: loreConfigDir },
    args: ["api", "request", "--json", "--method", "POST", "--path", "/v1/sessions"],
    serverURL: loreServerURL,
  }),
};

// Broker contract: lore api request --json --method POST --path /v1/sessions
