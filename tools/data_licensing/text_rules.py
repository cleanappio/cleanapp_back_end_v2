#!/usr/bin/env python3
"""Conservative triage from EXISTING AI annotation text only.

Does not read images, run a model, or access a database. Clear means the stored
annotation explicitly supports this compartment category; it is not a fresh
visual finding or human ground truth. Ambiguous results are never auto-selected.
"""
from __future__ import annotations

import json
import re
import sys

VERSION = "cleanapp-existing-text-quality-20261107-v2"


def rx(pattern): return re.compile(pattern, re.I)


WHOLE = r"(?:image|photo(?:graph)?|picture|frame|input(?:\s+image)?|uploaded\s+file)"
ABSOLUTE = r"(?:completely|entirely|totally|fully|uniformly|solid)"
BLACK = [
    rx(rf"\b{WHOLE}\b\s*(?:provided\s+|submitted\s+)?(?:is\s+|appears\s+(?:to\s+be\s+)?|looks\s+)?{ABSOLUTE}\s+(?:black|dark)\b"),
    rx(rf"\b{ABSOLUTE}\s+(?:black|dark)\s+{WHOLE}\b"),
]
BLANK = [rx(rf"\b(?:blank|empty|featureless)\s+{WHOLE}\b"),
         rx(rf"\b{WHOLE}\s+(?:is|appears)\s+(?:completely\s+|entirely\s+)?(?:blank|empty|featureless)\b")]
NO_CONTENT = [
    rx(r"\bno\s+(?:visible|discernible|recognizable|identifiable|meaningful|usable|distinct)(?:\s+visual)?\s+(?:content|objects?|features?|details?|scene|information|elements?)\b"),
    rx(r"\bwithout\s+(?:any\s+)?(?:visible|discernible|recognizable|identifiable|meaningful)\s+(?:content|objects?|features?|details?)\b"),
    rx(r"\bnothing\s+(?:is\s+)?(?:visible|discernible|recognizable|identifiable)\b"),
    rx(r"\b(?:cannot|can.t|unable\s+to)\s+(?:identify|recognize|discern|see)\s+(?:any\s+)?(?:objects?|content|features?|details?)\b"),
    rx(r"\bno\s+(?:objects?|features?|details?)\s+can\s+be\s+(?:identified|recognized|discerned|seen)\b"),
    rx(r"\b(?:impossible|unable)\s+to\s+(?:identify|recognize|discern|detect)\s+(?:any\s+)?(?:physical\s+)?(?:objects?|content|features?|details?)\b"),
    rx(r"\b(?:lacks?|lacking)\s+(?:any\s+)?(?:visible|discernible|recognizable)\s+(?:content|objects?|features?|details?|information|elements?)\b"),
    rx(r"\b0\s*%\s*(?:visible\s+)?(?:detail|content|visibility)\b"),
]
WHOLE_BLUR = [
    rx(rf"\b{WHOLE}\s+(?:is|appears|looks)\s+(?:extremely|severely|completely|entirely|heavily)\s+(?:blurred|blurry|out\s+of\s+focus)\b"),
    rx(rf"\b(?:extremely|severely|completely|entirely|heavily)\s+(?:blurred|blurry|out.of.focus)\s+{WHOLE}\b"),
    rx(rf"\b(?:entire|whole)\s+{WHOLE}\s+(?:is\s+)?(?:blurred|blurry|out\s+of\s+focus)\b"),
]
LOCAL_BLUR = [rx(r"\b(?:blurred|blurry|out.of.focus)\s+(?:background|sign(?:age)?|text|logo|license\s+plate)\b"),
              rx(r"\b(?:background|sign(?:age)?|text|logo|license\s+plate)\s+(?:is|appears)\s+(?:blurred|blurry|unreadable|illegible|out\s+of\s+focus)\b")]
PORTRAIT = [rx(r"\b(?:selfies?|self.portraits?|headshots?|portrait(?:\s+photo(?:graph)?)?)\b")]
PERSON_OR_SELFIE = [rx(r"\b(?:selfies?|self.portraits?|headshots?|man|woman|boy|girl|person|teenager|child|adult|face)\b")]
UNRELATED = [rx(r"\b(?:unrelated|irrelevant|non.relevant)\s+(?:personal\s+)?(?:selfie|portrait|headshot)\b"),
             rx(r"\b(?:unrelated|irrelevant)\s+to\s+(?:CleanApp|cleanliness|environmental|physical|digital|issue|problem|defect|hazard)\b")]
NO_ISSUE = [
    rx(r"\bno\s+(?:visible\s+|apparent\s+|discernible\s+|identifiable\s+|obvious\s+)?(?:defects?|hazards?|litter|issues?|problems?)(?:\s+(?:are\s+|is\s+)?visible)?\b"),
    rx(r"\b(?:does\s+not|doesn.t)\s+(?:show|depict|contain)\s+(?:any\s+)?(?:visible\s+)?(?:defects?|hazards?|litter|issues?|problems?)\b"),
    rx(r"\bno\s+signs\s+of\s+(?:defects?|hazards?|litter|issues?|problems?)\b"),
]
USEFUL = [
    rx(r"\b(?:visible|showing|depicting|containing|shows|depicts|contains)\b.{0,45}\b(?:litter|garbage|trash|debris|waste|pothole|crack|obstruction|hazard|damage|defect|error\s+(?:code|message)|warning|leak)\b"),
    rx(r"\b(?:litter|garbage|trash|debris|waste|pothole|crack|obstruction|hazard|damage|defect)\b\s+(?:is\s+|are\s+)?(?:visible|present|identifiable|evident)\b"),
    rx(r"\b(?:cracked|broken|damaged|blocked|overflowing|leaking)\s+(?:road|wall|pavement|sidewalk|window|door|toilet|seat|pipe|bin|drain|exit|path|ramp|screen|device|equipment)\b"),
    rx(r"\b(?:injur(?:y|ies|ed)|wounds?|rash|burns?|bruises?)\b"),
    rx(r"\b(?:person|man|woman|boy|girl)\b.{0,40}\b(?:holding|pointing|demonstrating|reporting)\b"),
    rx(r"\bwet\s+(?:(?:and|or)\s+uneven\s+)?(?:ground|floor|surface|road|pavement)\b"),
    rx(r"\b(?:potential|possible|poses?|posing|presents?|presenting)\b.{0,45}\b(?:slip|trip|fall|leak|hazard|risk|flood)\b"),
    rx(r"\b(?:torn|ripped|loose|dirty|smudged|stained|chipped|uneven)\b.{0,30}\b(?:roof|cover|wall|floor|mirror|wire|rope|terrain|ground|surface|stone|stones)\b"),
    rx(r"\b(?:wire|rope|cable)\b.{0,35}\b(?:ground|floor|trip|loose)\b"),
    rx(r"\b(?:residue|standing\s+water|flooded\s+field|loose\s+stones?|fly\s+on|smudges?)\b"),
    rx(r"\b(?:paper|cardboard)\b.{0,35}\b(?:floor|ground|litter)\b"),
]
PORTRAIT_CONTEXT = [rx(r"\b(?:screenshots?|screen\s+shot|lock\s*screen|user\s+interface|profile\s+(?:page|interface|ui)|app\s+icons?|camera\s+(?:ui|interface)|settings\s+(?:page|interface)|social\s+(?:app|media)|flyer|poster|brochure|printed\s+document|receipt|document)\b")]
EXCEPTION = [rx(r"\b(?:except(?:\s+for)?|apart\s+from|other\s+than)\b"),
             rx(r"\b(?:but|however)\b.{0,40}\b(?:visible|discernible|identifiable|shows|contains)\b")]
NEGATION_BEFORE = rx(r"\b(?:not|never|no|without|absence\s+of|zero)\b[^.!?;:\n]{0,45}$")


def matches(fields, patterns, reject_negated=False):
    out = []
    for field, text in fields.items():
        for pattern in patterns:
            for match in pattern.finditer(text):
                prefix = text[max(0, match.start()-60):match.start()]
                prefix = re.split(r"\b(?:but|however|although|except|yet)\b", prefix, flags=re.I)[-1]
                if reject_negated and NEGATION_BEFORE.search(prefix):
                    continue
                out.append({"field": field, "span": [match.start(), match.end()], "text": match.group()})
    return out


def classify_existing_text(title=None, description=None, summary=None, **context):
    fields = {"title": str(title or ""), "description": str(description or ""), "summary": str(summary or "")}
    result = {"rule_version": VERSION, "decision": "nonmatch", "category": None,
              "sequester_recommended": False, "evidence": [], "blocking_evidence": [],
              "review_method": "deterministic_rules_on_existing_AI_annotations",
              "new_vision_performed": False, "new_model_calls": 0,
              "original_image_inspected": False}
    if not any(text.strip() for text in fields.values()):
        return {**result, "decision": "ambiguous", "reason": "missing_existing_annotation_text"}
    black = matches(fields, BLACK, True)
    blank = matches(fields, BLANK, True)
    no_content = matches(fields, NO_CONTENT)
    blur = matches(fields, WHOLE_BLUR, True)
    portrait = matches(fields, PORTRAIT, True)
    portrait_focus = matches(fields, PERSON_OR_SELFIE, True)
    unrelated = matches(fields, UNRELATED, True)
    no_issue = matches(fields, NO_ISSUE)
    useful = matches(fields, USEFUL, True)
    portrait_context = matches(fields, PORTRAIT_CONTEXT, True)
    exceptions = matches(fields, EXCEPTION)
    localized_blur = matches(fields, LOCAL_BLUR)
    categories = []
    if black or blank:
        categories.append(("black_or_blank", black + blank + no_content,
                           bool(black or no_content), useful + exceptions))
    if blur:
        categories.append(("irrecoverable_whole_image_blur", blur + no_content,
                           bool(no_content), useful + exceptions + localized_blur))
    if portrait:
        categories.append(("unrelated_portrait_or_selfie", portrait + portrait_focus + unrelated + no_issue,
                           bool(portrait_focus and (unrelated or no_issue)), useful + portrait_context))
    if not categories:
        # Local blur or a black-colored object alone is not an exclusion lead.
        return {**result, "blocking_evidence": useful + portrait_context,
                "reason": "no_explicit_whole_image_quality_or_portrait_claim"}
    clear = [item for item in categories if item[2] and not item[3]]
    if len(clear) == 1:
        category, evidence, _, blockers = clear[0]
        return {**result, "decision": "clear", "category": category,
                "sequester_recommended": True, "evidence": evidence,
                "reason": "explicit_existing_annotation_support_without_detected_contradiction"}
    evidence = [span for item in categories for span in item[1]]
    blockers = [span for item in categories for span in item[3]]
    return {**result, "decision": "ambiguous",
            "category": categories[0][0] if len(categories) == 1 else "multiple_annotation_categories",
            "evidence": evidence, "blocking_evidence": blockers,
            "reason": "contradictory_or_insufficient_existing_annotation"}


def main():
    for line in sys.stdin:
        if not line.strip(): continue
        record = json.loads(line)
        result = classify_existing_text(**record)
        print(json.dumps({"seq": record.get("seq", record.get("report_id")), **result}, ensure_ascii=False))


if __name__ == "__main__":
    main()
