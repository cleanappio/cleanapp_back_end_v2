"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.getFilter = getFilter;
function getFilter(filter) {
  if (!Array.isArray(filter) || filter.length === 0) {
    return [];
  }
  return filter;
}
//# sourceMappingURL=filterUtils.js.map