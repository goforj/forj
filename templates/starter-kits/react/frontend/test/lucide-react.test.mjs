import assert from "node:assert/strict"
import test from "node:test"
import { createElement } from "react"
import { renderToStaticMarkup } from "react-dom/server"
import { GitFork } from "lucide-react"

test("Lucide repository icons follow the v1 accessibility contract", () => {
  const decorative = renderToStaticMarkup(createElement(GitFork))
  assert.match(decorative, /aria-hidden="true"/)

  const accessible = renderToStaticMarkup(
    createElement(GitFork, { "aria-label": "Repository", role: "img" }),
  )
  assert.doesNotMatch(accessible, /aria-hidden=/)
  assert.match(accessible, /aria-label="Repository"/)
})
