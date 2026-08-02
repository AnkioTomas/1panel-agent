#!/usr/bin/env python3
from http.server import BaseHTTPRequestHandler, HTTPServer


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path.split("?", 1)[0] == "/hello":
            body = b"panel-ok"
            self.send_response(200)
            self.send_header("Content-Type", "text/plain")
            self.send_header("Content-Length", str(len(body)))
            self.send_header("X-Echo-Token", self.headers.get("1Panel-Token", ""))
            self.send_header("X-Echo-Ts", self.headers.get("1Panel-Timestamp", ""))
            self.send_header("Set-Cookie", "sid=1; Path=/")
            self.end_headers()
            self.wfile.write(body)
            return
        if self.path.split("?", 1)[0] in ("/", "/index.html"):
            body = b"<html><body>fake-1panel</body></html>"
            self.send_response(200)
            self.send_header("Content-Type", "text/html")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        self.send_response(404)
        self.end_headers()

    def log_message(self, fmt, *args):
        return


if __name__ == "__main__":
    HTTPServer(("0.0.0.0", 20560), Handler).serve_forever()
