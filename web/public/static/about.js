(function () {
  "use strict";
  const root = document.querySelector(".about-stack");
  const i18n = window.MirrorI18n;
  if (!root || !i18n) return;

  function t(key, params) { return i18n.t(key, params); }

  function setText(node, key, params) {
    if (node) node.textContent = t(key, params);
  }

  function localize() {
    const cards = root.querySelectorAll(":scope > .about-card");
    if (cards[0]) {
      setText(cards[0].querySelector(".about-card__title h2"), "about.intro");
      setText(cards[0].querySelector("p"), "page.subtitle.page-download");
    }
    if (cards[1]) {
      setText(cards[1].querySelector(".about-card__title h2"), "about.sponsorSupport");
      const paragraph = cards[1].querySelector("p");
      if (paragraph) {
        const note = document.createElement("strong");
        const support = document.createElement("strong");
        note.textContent = t("about.sponsorNote");
        support.textContent = t("about.supportStrong");
        paragraph.replaceChildren(note, document.createTextNode(t("about.supportPrefix") + " "),
          support, document.createTextNode(" " + t("about.supportSuffix")));
      }
      const images = cards[1].querySelectorAll("img");
      if (images[0]) images[0].alt = t("pow.wechat");
      if (images[1]) images[1].alt = t("pow.alipay");
      const headings = cards[1].querySelectorAll("h3");
      if (headings[0]) setText(headings[0], "pow.wechat");
      if (headings[1]) setText(headings[1], "pow.alipay");
    }

    const thanks = root.querySelector(".about-section");
    if (thanks) {
      setText(thanks.querySelector(":scope > h2"), "about.thanks");
      setText(thanks.querySelector(".thanks-card h3"), "about.design");
      const paragraph = thanks.querySelector(".thanks-card p");
      const link = paragraph && paragraph.querySelector("a");
      if (paragraph && link) {
        paragraph.replaceChildren(document.createTextNode(t("about.designPrefix")), link,
          document.createTextNode(t("about.designSuffix")));
      }
    }

    const contribution = cards[2];
    if (contribution) {
      setText(contribution.querySelector("h2"), "about.contribution");
      const paragraph = contribution.querySelector("p");
      const email = paragraph && paragraph.querySelector("a");
      if (paragraph && email) {
        paragraph.replaceChildren(document.createTextNode(t("about.contributionPrefix")), email);
      }
    }

    const sponsor = document.getElementById("sponsors");
    if (!sponsor) return;
    setText(sponsor.querySelector(".sponsor-card__head h2"), "about.sponsors");
    const count = sponsor.querySelector(".sponsor-card__head > span");
    if (count) {
      const match = count.textContent.match(/\d+/);
      count.textContent = t("about.sponsorCount", {count: match ? match[0] : "0"});
    }
    setText(sponsor.querySelector("#sponsor-featured-title"), "about.sponsorFeatured");
    setText(sponsor.querySelector(".sponsor-card > .empty"), "about.sponsorEmpty");
    const list = sponsor.querySelector(".sponsor-list:not(.sponsor-list--featured)");
    if (list) list.setAttribute("aria-label", t("about.sponsorRecords"));
    const pager = sponsor.querySelector(".sponsor-pager");
    if (pager) {
      pager.setAttribute("aria-label", t("about.sponsorPager"));
      const controls = pager.querySelectorAll(".sponsor-pager__control");
      if (controls[0]) setText(controls[0], "about.previous");
      if (controls[controls.length - 1]) setText(controls[controls.length - 1], "about.next");
    }
    sponsor.querySelectorAll(".sponsor-badge--pinned").forEach((node) => setText(node, "about.sponsorPinned"));
    sponsor.querySelectorAll(".sponsor-badge--count").forEach((node) => {
      const match = node.textContent.match(/\d+/);
      setText(node, "about.sponsorMonthCount", {count: match ? match[0] : "0"});
    });
  }

  localize();
  i18n.onChange(localize);
})();
