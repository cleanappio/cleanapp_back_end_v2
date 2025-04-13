"use strict";

/**
 * deprecatedClass: creates a subclass of the class, which prints deprecated warning when called
 */
export function deprecatedClass(origClass, deprecationMessage) {
  const result = class extends origClass {
    constructor(...args) {
      console.log(`Deprecated: ${deprecationMessage}`);
      super(...args);
    }
  };
  return result;
}

/**
 * Copy properties from origObject to newObject, but only those which not exists in newObject.
 * Calls onDeprecatedCalled callback in case a copied property is invoked.
 */
export function copyPropertiesAsDeprecated(origObject, newObject, onDeprecatedCalled, accessors = {}) {
  const result = newObject;
  for (const [key, value] of Object.entries(origObject)) {
    if (!newObject[key]) {
      Object.defineProperty(result, key, {
        get() {
          onDeprecatedCalled(key);
          return accessors[key] ? accessors[key](value) : value;
        }
      });
    }
  }
  return result;
}
//# sourceMappingURL=deprecation.js.map