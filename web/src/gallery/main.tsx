// Entry point of the design gallery (gallery.html). Not part of the app
// that ships on the device.

import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "../styles.css";
import "./gallery.css";
import { Gallery } from "./Gallery";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <Gallery />
  </StrictMode>,
);
