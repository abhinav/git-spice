"""
Adds a badgge shortcode in the form:

    <!-- gs:badge ICON TEXT -->

Where ICON is a mkdocs-material icon name
and TEXT is the text to display.
"""

import re

from mkdocs.config.defaults import MkDocsConfig
from mkdocs.structure.files import Files
from mkdocs.structure.pages import Page

_tag_re = re.compile(r'<!-- gs:badge ([^ ]+) (.+?) -->')


def on_page_markdown(
    markdown: str,
    page: Page,
    config: MkDocsConfig,
    files: Files,
    **kwargs
) -> str:
    def replace(match: re.Match) -> str:
        icon = match.group(1)
        text = match.group(2)
        return ''.join([
            '<span class="mdx-badge">',
            *[
                '<span class="mdx-badge__icon">',
                f':{icon}:',
                '</span>',
            ],
            *[
                '<span class="mdx-badge__text">',
                text,
                '</span>',
            ],
            '</span>'
        ])

    return _tag_re.sub(replace, markdown)
