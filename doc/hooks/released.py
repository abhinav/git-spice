"""
Adds support for shortcodes in the form:

    <!-- gs:version VERSION -->

Where VERSION is a version number of "unreleasd".
Will replace with a link to the corresponding changelog scetion.
"""

import re

from mkdocs.config.defaults import MkDocsConfig
from mkdocs.structure.files import Files
from mkdocs.structure.pages import Page


_tag_re = re.compile(r'<!-- gs:version ([^ ]+) -->')


def on_page_markdown(
    markdown: str,
    page: Page,
    config: MkDocsConfig,
    files: Files,
    **kwargs
) -> str:

    def replace(match: re.Match) -> str:
        version = match.group(1)
        icon = ":material-tag:"
        if version == "unreleased":
            icon = ":material-tag-hidden:"

        href = f'/changelog.md#{version}'
        text = version
        if version == "unreleased":
            href = ''
            text = 'Unreleased'

        return ''.join([
            '<span class="mdx-badge">',
            *[
                '<span class="mdx-badge__icon">',
                f'{icon}{{ title="Released in version" }}',
                '</span>',
            ],
            *[
                '<span class="mdx-badge__text">',
                f'[{text}]({href})' if href else text,
                '</span>',
            ],
            '</span>'
        ])

    return _tag_re.sub(replace, markdown)
