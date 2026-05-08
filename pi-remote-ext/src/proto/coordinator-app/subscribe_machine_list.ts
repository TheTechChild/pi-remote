// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/coordinator-app/subscribe_machine_list.json

/**
 * App -> Coordinator. Subscribe to machine and session updates. See SPEC.md § 10.3.
 */
export interface SubscribeMachineList {
  type: "subscribe_machine_list";
  v: 1;
}
