#!/bin/sh
# Example sockdock command handler.
# stdin:  the event JSON (see README "Handlers")
# stdout: the reply posted to the Slack thread
input=$(cat)
body=$(printf '%s' "$input" | sed -n 's/.*"body":"\([^"]*\)".*/\1/p')
echo "You said: ${body:-(nothing)} (turn ${SOCKDOCK_TURN}, route ${SOCKDOCK_ROUTE})"
