# Introduces a $$...$$ syntax for references to CLI commands in the reference.
# $$command|text$$ will use {text} as the link text.

import re
from mkdocs.structure.pages import Page
from mkdocs.config.defaults import MkDocsConfig
from mkdocs.structure.files import Files


_CLI_PAGE = "cli/index.md"
_cmd_re = re.compile(r"\$\$([^$]+)\$\$")


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
        cmd = match.group(1)
        text = cmd
        if "|" in cmd:
            cmd, text = cmd.split("|", 1)
        id = cmd.replace(" ", "-")
        return f'[:material-console:{{ .middle }} {text}](/{_CLI_PAGE}#{id})'

    return _cmd_re.sub(_replace, markdown)
