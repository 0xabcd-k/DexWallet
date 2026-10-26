import type { EchoRequest, EchoResponse } from "@dexwallet/api-contract/example";

const request: EchoRequest = {
  message: "example",
  amount: "9007199254740993.123456789012345678",
  items: [{ kind: "counter", label: "zero", sequence: 0 }],
};
const response: EchoResponse = request;
const amount: string = response.amount;

// @ts-expect-error Amounts must remain strings, even when a number fits in JS.
const wrongAmount: EchoResponse = { ...response, amount: 0.1 };
// @ts-expect-error The contract omits absent notes; it does not serialize null.
const nullNote: EchoResponse = { ...response, note: null };
// @ts-expect-error Empty arrays are [], not null.
const nullItems: EchoResponse = { ...response, items: null };
// @ts-expect-error Missing required fields must not become any/optional.
const missingAmount: EchoRequest = { message: "example", items: [] };
void [amount, wrongAmount, nullNote, nullItems, missingAmount];

// @ts-expect-error Enum values must come from the .api definition.
const wrongKind: EchoRequest = { ...request, items: [{ kind: "unknown", label: "x", sequence: 0 }] };
void wrongKind;
