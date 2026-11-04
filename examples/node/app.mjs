import express from "express";
import { auth } from "express-oauth2-jwt-bearer";

export function createApp({ issuerBaseURL, audience }) {
  const app = express();
  app.get("/private", auth({ issuerBaseURL, audience }), (req, res) => {
    res.json({ sub: req.auth.payload.sub });
  });
  return app;
}
