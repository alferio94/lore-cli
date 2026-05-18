const loreBinaryPath = "{{LORE_BINARY_PATH}}";
const loreConfigDir = "{{LORE_CONFIG_DIR}}";
const loreServerURL = "{{LORE_SERVER_URL}}";
const loreCliVersion = "{{LORE_CLI_VERSION}}";

export const lore_footer = {
  name: "lore_footer",
  status: "cli-request",
  details: `Remote Lore ${loreServerURL} via ${loreBinaryPath} (${loreCliVersion}) with lore api request and LORE_CONFIG_DIR=${loreConfigDir}`,
};
