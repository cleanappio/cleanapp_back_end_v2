"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.default = checkRequiredProps;
function checkRequiredProps(tag, props, required) {
  for (const key of required) {
    if (props[key] === undefined) {
      console.error(`Error: ${tag} property: ${key} is required but it was missing.`);
    }
  }
}
//# sourceMappingURL=checkRequiredProps.js.map