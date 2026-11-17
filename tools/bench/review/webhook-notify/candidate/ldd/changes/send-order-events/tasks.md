# send-order-events

## Intent

Send each order event to the partner's webhook, authenticated with the partner's API token, and return the partner's receipt.

Delivers: notify.token_sender.send_event

- [x] 1.1 build the signed request
- [x] 1.2 deliver it and read the receipt
