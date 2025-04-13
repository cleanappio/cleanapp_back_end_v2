"use strict";

export default function checkRequiredProps(tag, props, required) {
  for (const key of required) {
    if (props[key] === undefined) {
      console.error(`Error: ${tag} property: ${key} is required but it was missing.`);
    }
  }
}
//# sourceMappingURL=checkRequiredProps.js.map