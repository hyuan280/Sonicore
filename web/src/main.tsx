import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import "./i18n";
import { i18nReady } from "./i18n";
import App from "./App";
import "./index.css";

function renderApp() {
  createRoot(document.getElementById("root")!).render(
    <StrictMode>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </StrictMode>,
  );
}

i18nReady.then(renderApp).catch((err) => {
  console.error("Failed to load locale, rendering with fallback", err);
  renderApp();
});
