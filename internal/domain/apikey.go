package domain

import "errors"

// APIKeyPrefix is the textual prefix every raw external API key carries
// ("fbk_live_<32 hex>"). The prefix lets the middleware reject malformed
// headers cheaply before hitting the database.
const APIKeyPrefix = "fbk_live_"

// ErrInvalidAPIKey signals a request whose API key did not match any
// active row. service.APIKeys returns it; the middleware maps it to 401
// while everything else maps to 500.
var ErrInvalidAPIKey = errors.New("invalid api key")
