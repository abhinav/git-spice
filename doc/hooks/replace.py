"""
Simple text replacement hooks.
"""

from mkdocs.config.defaults import MkDocsConfig
from mkdocs.structure.files import Files
from mkdocs.structure.pages import Page


REPLACEMENTS = {
    '<!-- gs:github -->': ':simple-github: GitHub',
    '<!-- gs:gitlab -->': ':simple-gitlab: GitLab',
    '<!-- gs:badge:github ': '<!-- gs:badge simple-github GitHub ',
    '<!-- gs:badge:gitlab ': '<!-- gs:badge simple-gitlab GitLab ',
}


def on_page_markdown(
    markdown: str,
    page: Page,
    config: MkDocsConfig,
    files: Files,
    **kwargs
) -> str:
    for key, value in REPLACEMENTS.items():
        markdown = markdown.replace(key, value)
    return markdown
