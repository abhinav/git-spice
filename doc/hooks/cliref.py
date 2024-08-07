"""
Introduces a $$...$$ syntax for references to CLI commands or configuration
in documentation

The syntax is:

- $$gs *$$ will produce a link to the CLI reference page.
- $$spice.*$$ will produce a link to the configuration reference page.

By default, $$foo$$ will use {foo} as the link text.
The form $$foo|text$$ will use {text} as the link text,
while still linking to {foo}.
"""

import re
from mkdocs.structure.pages import Page
from mkdocs.config.defaults import MkDocsConfig
from mkdocs.structure.files import Files


_CLI_PAGE = "cli/reference.md"
_CONFIG_PAGE = "cli/config.md"
_re = re.compile(r"\$\$([^$]+)\$\$")


def on_page_markdown(
    markdown: str,
    page: Page,
    config: MkDocsConfig,
    files: Files,
) -> str:
    # Don't process the target page itself.
    if page.file.src_uri == _CLI_PAGE:
        return markdown

    def _replace(match):
        title = match.group(1)
        text = title
        if "|" in title:
            title, text = title.split("|", 1)

        if title.startswith("gs "):
            icon = ":material-console:"
            id = title.replace(" ", "-")
            page = _CLI_PAGE
        elif title.startswith("spice."):
            icon = ":material-wrench:"
            id = title.replace(".", "").lower()
            page = _CONFIG_PAGE
        else:
            return match.group(0)  # No match, return as is.

        return f'[{icon}{{ .middle }} {text}](/{page}#{id})'

    return _re.sub(_replace, markdown)
