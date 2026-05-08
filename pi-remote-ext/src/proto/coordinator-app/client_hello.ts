// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/coordinator-app/client_hello.json

/**
 * App -> Coordinator. First message after WebSocket open. See SPEC.md § 10.3.
 */
export interface ClientHello {
  type: "client_hello";
  v: 1;
  client_id: string;
  app_version: string;
}
