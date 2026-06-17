(() => {
	// Show/hide the TOTP code like a password field.
	var password = document.getElementById("password");
	var toggleBtn = document.getElementById("toggle-password");
	var eyeOpen = document.getElementById("eye-open");
	var eyeClosed = document.getElementById("eye-closed");

	toggleBtn.addEventListener("click", () => {
		var isPassword = password.type === "password";
		password.type = isPassword ? "text" : "password";
		toggleBtn.setAttribute(
			"aria-label",
			isPassword ? "Hide code" : "Show code",
		);
		eyeOpen.style.display = isPassword ? "none" : "inline";
		eyeClosed.style.display = isPassword ? "inline" : "none";
	});
})();
