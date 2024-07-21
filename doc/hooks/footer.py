import logging
from os.path import normpath

from mkdocs.structure.pages import Page
from mkdocs.utils.templates import TemplateContext
from mkdocs.config.defaults import MkDocsConfig
from mkdocs.structure.nav import Navigation

log = logging.getLogger("mkdocs.plugins.footer")

# A variant of the footer functionality in mkdocs-material
# but with support for pages to define their own next_page or prev_page
# in front matter.
#
# Useful for pages that are not part of the main nav,
# but still want to have a next/prev page.


def on_page_context(
    ctx: TemplateContext,
    page: Page,
    config: MkDocsConfig,
    nav: Navigation,
) -> TemplateContext:
    next_page_name = page.meta.get('next_page', None)
    prev_page_name = page.meta.get('prev_page', None)
    if not next_page_name and not prev_page_name:
        return ctx

    # foo/bar/baz.md -> foo/bar
    uri = page.file.src_uri
    dir = '/'.join(uri.split('/')[:-1])

    next_page_uri, prev_page_uri = None, None
    if next_page_name:
        next_page_uri = normpath(f'{dir}/{next_page_name}')
    if prev_page_name:
        prev_page_uri = normpath(f'{dir}/{prev_page_name}')

    next_page, prev_page = None, None
    for file in ctx['pages']:
        uri = file.src_uri
        if next_page_uri and uri == next_page_uri:
            next_page = file.page
        if prev_page_uri and uri == prev_page_uri:
            prev_page = file.page

    if next_page_name and next_page is None:
        log.warn(f'page {next_page_uri} not found')
    if prev_page_name and prev_page is None:
        log.warn(f'page {prev_page_uri} not found')

    if not hasattr(page, 'overrides'):
        page.overrides = {}

    if next_page:
        page.next_page = next_page
    if prev_page:
        page.previous_page = prev_page
    return ctx
