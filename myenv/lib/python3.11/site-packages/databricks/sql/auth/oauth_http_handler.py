from http.server import BaseHTTPRequestHandler


class OAuthHttpSingleRequestHandler(BaseHTTPRequestHandler):
    RESPONSE_BODY_TEMPLATE = """<html>
<head>
  <title>Close this Tab</title>
  <style>
    body {
      font-family: "Barlow", Helvetica, Arial, sans-serif;
      padding: 20px;
      background-color: #f3f3f3;
    }
  </style>
</head>
<body>
  <h1>Please close this tab.</h1>
  <p>
    The {!!!PLACE_HOLDER!!!} received a response. You may close this tab.
  </p>
</body>
</html>"""

    def __init__(self, tool_name):
        self.response_body = self.RESPONSE_BODY_TEMPLATE.replace(
            "{!!!PLACE_HOLDER!!!}", tool_name
        ).encode("utf-8")
        self.request_path = None

    def __call__(self, *args, **kwargs):
        """Handle a request."""
        super().__init__(*args, **kwargs)

    def do_GET(self):  # nopep8
        self.send_response(200, "Success")
        self.send_header("Content-type", "text/html")
        self.end_headers()
        self.wfile.write(self.response_body)
        self.request_path = self.path

    def log_message(self, format, *args):
        # pylint: disable=redefined-builtin
        # pylint: disable=unused-argument
        return
