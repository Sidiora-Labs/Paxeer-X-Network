import React, { useEffect, useState } from "react";
import { styled } from "storybook/theming";

const StyledUl = styled.ul`
  list-style: none;
  padding: 30px 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
  align-items: end;
  font-size: 13px;
  position: sticky;
  top: 30px;
`;

const StyledA = styled.a<{ active: boolean }>`
  text-decoration: none;
  color: ${(props) => (props.active ? "#b084e9" : "inherit")};
  cursor: pointer;
`;

const sections = [
  { id: "colors", label: "Colors" },
  { id: "radius", label: "Radius" },
  { id: "typography", label: "Typography" },
];
export const ThemeMenu = () => {
  const [activeSection, setActiveSection] = useState("");

  useEffect(() => {
    const observer = new IntersectionObserver(
      (entries) => {
        entries.forEach((entry) => {
          if (entry.isIntersecting) {
            setActiveSection(entry.target.id);
          }
        });
      },
      { threshold: 0.2, rootMargin: "-20px 0px 0px 0px" },
    );

    sections.forEach((section) => {
      const element = document.getElementById(section.id);
      if (element) observer.observe(element);
    });

    return () => observer.disconnect();
  }, []);

  const handleClick = (id: string) => (e: React.MouseEvent) => {
    e.preventDefault();
    document.getElementById(id)?.scrollIntoView({ behavior: "smooth" });
  };

  return (
    <StyledUl>
      {sections.map(({ id, label }) => (
        <li key={id}>
          <StyledA onClick={handleClick(id)} active={activeSection === id}>
            {label}
          </StyledA>
        </li>
      ))}
    </StyledUl>
  );
};
