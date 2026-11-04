import express from "express";
import { auth } from "express-oauth2-jwt-bearer";

export function createApp({ issuerBaseURL, audience }) {
  const app = express();
  app.get("/private", auth({ issuerBaseURL, audience }), (req, res) => {
    res.json({ sub: req.auth.payload.sub });
  });
  app.use((err, req, res, next) => {
    res.status(err.status ?? 500).json({ error: err.code ?? "server_error" });
  });
  return app;
}
