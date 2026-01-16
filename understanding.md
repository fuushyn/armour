This is an MCP proxy. It is a single upstream server you can configure (connect to claude via stdio). 
Instead of registering new servers directly with claude, you register it with the proxy server.
stdio servers are proxied via stdio
http servers - the proxy stdio server sends http in input pipe and takes response in output pipe (how??)
it uses the session start hook to move the server config from claude to proxy. this way you just connect to one upstream.

Things that might need handling to make sure it mirrors the official MCP spec 100% (https://modelcontextprotocol.io/docs)
- Need to connect to several mcp servers to find edge cases (eg context 7 server expected a header we were not passing)
- need to see edge cases or conditions where the translatinos might break (stdio-> stdio or stdio->http)
- mcp auth
- elicitation, notification and sampling

what's next
- add runtime guardrails and make a nice control panel
- ship this to github
- think about enterprise features to make this an easy sell
